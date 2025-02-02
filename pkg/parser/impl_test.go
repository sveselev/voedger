/*
* Copyright (c) 2023-present unTill Pro, Ltd.
* @author Michael Saigachenko
 */
package parser

import (
	"embed"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
)

//go:embed sql_example_app/pmain/*.sql
var fsMain embed.FS

//go:embed sql_example_app/airsbp/*.sql
var fsAir embed.FS

//go:embed sql_example_app/untill/*.sql
var fsUntill embed.FS

//go:embed sql_example_syspkg/*.sql
var sfs embed.FS

//go:embed sql_example_app/vrestaurant/*.sql
var fsvRestaurant embed.FS

//_go:embed example_app/expectedParsed.schema
//var expectedParsedExampledSchemaStr string

func getSysPackageAST() *PackageSchemaAST {
	pkgSys, err := ParsePackageDir(appdef.SysPackage, sfs, "sql_example_syspkg")
	if err != nil {
		panic(err)
	}
	return pkgSys
}

func Test_BasicUsage(t *testing.T) {

	require := require.New(t)
	mainPkgAST, err := ParsePackageDir("github.com/untillpro/main", fsMain, "sql_example_app/pmain")
	require.NoError(err)

	airPkgAST, err := ParsePackageDir("github.com/untillpro/airsbp", fsAir, "sql_example_app/airsbp")
	require.NoError(err)

	untillPkgAST, err := ParsePackageDir("github.com/untillpro/untill", fsUntill, "sql_example_app/untill")
	require.NoError(err)

	// := repr.String(pkgExample, repr.Indent(" "), repr.IgnorePrivate())
	//fmt.Println(parsedSchemaStr)

	appSchema, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		mainPkgAST,
		airPkgAST,
		untillPkgAST,
	})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(appSchema, builder)
	require.NoError(err)

	// table
	cdoc := builder.CDoc(appdef.NewQName("main", "TablePlan"))
	require.NotNil(cdoc)
	require.Equal(appdef.TypeKind_CDoc, cdoc.Kind())
	require.Equal(appdef.DataKind_int32, cdoc.Field("FState").DataKind())
	require.Equal("Backoffice Table", cdoc.Comment())

	// TODO: sf := cdoc.Field("CheckedField").(appdef.IStringField)
	// TODO: require.Equal(uint16(8), sf.Restricts().MaxLen())
	// TODO: require.NotNil(sf.Restricts().Pattern())

	// container of the table
	container := cdoc.Container("TableItems")
	require.Equal("TableItems", container.Name())
	require.Equal(appdef.NewQName("main", "TablePlanItem"), container.QName())
	require.Equal(appdef.Occurs(0), container.MinOccurs())
	require.Equal(appdef.Occurs(maxNestedTableContainerOccurrences), container.MaxOccurs())
	require.Equal(appdef.TypeKind_CRecord, container.Type().Kind())
	require.Equal(2+5 /* +5 system fields*/, container.Type().(appdef.IFields).FieldCount())
	require.Equal(appdef.DataKind_int32, container.Type().(appdef.IFields).Field("TableNo").DataKind())
	require.Equal(appdef.DataKind_int32, container.Type().(appdef.IFields).Field("Chairs").DataKind())

	// constraint
	uniques := cdoc.Uniques()
	require.Equal(2, len(uniques))

	t.Run("first unique, automatically named", func(t *testing.T) {
		u := uniques[appdef.MustParseQName("main.TablePlan$uniques$01")]
		require.NotNil(u)
		cnt := 0
		for _, f := range u.Fields() {
			cnt++
			switch n := f.Name(); n {
			case "FState":
				require.Equal(appdef.DataKind_int32, f.DataKind())
			case "Name":
				require.Equal(appdef.DataKind_string, f.DataKind())
			default:
				require.Fail("unexpected field name", n)
			}
		}
		require.Equal(2, cnt)
	})

	t.Run("second unique, named by user", func(t *testing.T) {
		u := uniques[appdef.MustParseQName("main.TablePlan$uniques$UniqueTable")]
		require.NotNil(u)
		cnt := 0
		for _, f := range u.Fields() {
			cnt++
			switch n := f.Name(); n {
			case "TableNumber":
				require.Equal(appdef.DataKind_int32, f.DataKind())
			default:
				require.Fail("unexpected field name", n)
			}
		}
		require.Equal(1, cnt)
	})

	// child table
	crec := builder.CRecord(appdef.NewQName("main", "TablePlanItem"))
	require.NotNil(crec)
	require.Equal(appdef.TypeKind_CRecord, crec.Kind())
	require.Equal(appdef.DataKind_int32, crec.Field("TableNo").DataKind())

	crec = builder.CRecord(appdef.NewQName("main", "NestedWithName"))
	require.NotNil(crec)
	require.True(crec.Abstract())
	field := crec.Field("ItemName")
	require.NotNil(field)
	require.Equal("Field is added to any table inherited from NestedWithName\nThe current comment is also added to scheme for this field", field.Comment())

	// multinine comments
	singleton := builder.CDoc(appdef.NewQName("main", "SubscriptionProfile"))
	require.Equal("Singletones are always CDOC. Error is thrown on attempt to declare it as WDOC or ODOC\nThese comments are included in the statement definition, but may be overridden with `WITH Comment=...`", singleton.Comment())

	cmd := builder.Command(appdef.NewQName("main", "NewOrder"))
	require.Equal("Commands can only be declared in workspaces\nCommand can have optional argument and/or unlogged argument\nCommand can return TYPE", cmd.Comment())

	// type
	obj := builder.Object(appdef.NewQName("main", "SubscriptionEvent"))
	require.Equal(appdef.TypeKind_Object, obj.Kind())
	require.Equal(appdef.DataKind_string, obj.Field("Origin").DataKind())

	// view
	view := builder.View(appdef.NewQName("main", "XZReports"))
	require.NotNil(view)
	require.Equal(appdef.TypeKind_ViewRecord, view.Kind())
	require.Equal("VIEWs generated by the PROJECTOR.\nPrimary Key must be declared in View.", view.Comment())

	require.Equal(2, view.Value().UserFieldCount())
	require.Equal(1, view.Key().PartKey().FieldCount())
	require.Equal(4, view.Key().ClustCols().FieldCount())

	// workspace descriptor
	descr := builder.CDoc(appdef.NewQName("main", "MyWorkspaceDescriptor"))
	require.NotNil(descr)
	require.Equal(appdef.TypeKind_CDoc, descr.Kind())
	require.Equal(appdef.DataKind_string, descr.Field("Name").DataKind())
	require.Equal(appdef.DataKind_string, descr.Field("Country").DataKind())

	// fieldsets
	cdoc = builder.CDoc(appdef.NewQName("main", "WsTable"))
	require.Equal(appdef.DataKind_string, cdoc.Field("Name").DataKind())

	crec = builder.CRecord(appdef.NewQName("main", "Child"))
	require.Equal(appdef.DataKind_int32, crec.Field("Kind").DataKind())

	// QUERY
	q1 := builder.Query(appdef.NewQName("main", "_Query1"))
	require.NotNil(q1)
	require.Equal(appdef.TypeKind_Query, q1.Kind())

	// CUD Projector
	proj := builder.Projector(appdef.NewQName("main", "RecordsRegistryProjector"))
	require.NotNil(proj)
	eventsCount := 0
	proj.Events(func(ie appdef.IProjectorEvent) {
		eventsCount++
		k, on := ie.Kind(), ie.On().QName()
		require.Len(k, 3)
		require.Contains(k, appdef.ProjectorEventKind_Insert)
		require.Contains(k, appdef.ProjectorEventKind_Activate)
		require.Contains(k, appdef.ProjectorEventKind_Deactivate)
		switch eventsCount {
		case 1:
			require.Equal(istructs.QNameCRecord, on)
		case 2:
			require.Equal(istructs.QNameWRecord, on)
		}
	})
	require.Equal(2, eventsCount)

	// Execute Projector
	proj = builder.Projector(appdef.NewQName("main", "UpdateDashboard"))
	require.NotNil(proj)
	eventsCount = 0
	proj.Events(func(ie appdef.IProjectorEvent) {
		eventsCount++
		if eventsCount == 1 {
			require.Equal(1, len(ie.Kind()))
			require.Equal(appdef.ProjectorEventKind_Execute, ie.Kind()[0])
			require.Equal(appdef.NewQName("main", "NewOrder"), ie.On().QName())
		} else if eventsCount == 2 {
			require.Equal(1, len(ie.Kind()))
			require.Equal(appdef.ProjectorEventKind_Execute, ie.Kind()[0])
			require.Equal(appdef.NewQName("main", "NewOrder2"), ie.On().QName())
		}
	})

	stateCount := 0
	proj.States(func(storage appdef.QName, names appdef.QNames) {
		stateCount++
		if stateCount == 1 {
			require.Equal(appdef.NewQName("sys", "AppSecret"), storage)
			require.Equal(0, len(names))
		} else if stateCount == 2 {
			require.Equal(appdef.NewQName("sys", "Http"), storage)
			require.Equal(0, len(names))
		}
	})
	require.Equal(2, stateCount)

	intentsCount := 0
	proj.Intents(func(storage appdef.QName, names appdef.QNames) {
		intentsCount++
		if intentsCount == 1 {
			require.Equal(appdef.NewQName("sys", "View"), storage)
			require.Equal(4, len(names))
			require.Equal(appdef.NewQName("main", "ActiveTablePlansView"), names[0])
			require.Equal(appdef.NewQName("main", "DashboardView"), names[1])
			require.Equal(appdef.NewQName("main", "NotificationsHistory"), names[2])
			require.Equal(appdef.NewQName("main", "XZReports"), names[3])
		}
	})
	require.Equal(1, intentsCount)

	_, err = builder.Build()
	require.NoError(err)

}

type ParserAssertions struct {
	*require.Assertions
}

func (require *ParserAssertions) AppSchemaError(sql string, expectErrors ...string) {
	_, err := require.AppSchema(sql)
	require.EqualError(err, strings.Join(expectErrors, "\n"))
}

func (require *ParserAssertions) NoAppSchemaError(sql string) {
	_, err := require.AppSchema(sql)
	require.NoError(err)
}

func (require *ParserAssertions) AppSchema(sql string) (*AppSchemaAST, error) {
	ast, err := ParseFile("file.sql", sql)
	require.NoError(err)

	pkg, err := BuildPackageSchema("github.com/company/pkg", []*FileSchemaAST{ast})
	require.NoError(err)

	schema, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})

	return schema, err
}

func assertions(t *testing.T) *ParserAssertions {
	return &ParserAssertions{require.New(t)}
}

func Test_Refs_NestedTables(t *testing.T) {

	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	TABLE table1 INHERITS CDoc (
		items TABLE inner1 (
			table1 ref,
			ref1 ref(table3),
			urg_number int32
		)
	);
	TABLE table2 INHERITS CRecord (
	);
	TABLE table3 INHERITS CDoc (
		items table2
	);
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)
	adf := appdef.New()
	require.NoError(BuildAppDefs(packages, adf))
	inner1 := adf.Type(appdef.NewQName("pkg1", "inner1"))
	ref1 := inner1.(appdef.IFields).RefField("ref1")
	require.EqualValues(appdef.QNames{appdef.NewQName("pkg1", "table3")}, ref1.Refs())

	_, err = adf.Build()
	require.NoError(err)
}

func Test_CircularReferences(t *testing.T) {

	require := require.New(t)

	// Tables
	fs, err := ParseFile("file1.sql", `APPLICATION test();
	ABSTRACT TABLE table2 INHERITS table2 ();
	ABSTRACT TABLE table3 INHERITS table3 ();
	ABSTRACT TABLE table4 INHERITS table5 ();
	ABSTRACT TABLE table5 INHERITS table6 ();
	ABSTRACT TABLE table6 INHERITS table4 ();
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("pkg/test", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})

	require.EqualError(err, strings.Join([]string{
		"file1.sql:2:2: circular reference in INHERITS",
		"file1.sql:3:2: circular reference in INHERITS",
		"file1.sql:4:2: circular reference in INHERITS",
		"file1.sql:5:2: circular reference in INHERITS",
		"file1.sql:6:2: circular reference in INHERITS",
	}, "\n"))

	// Workspaces
	fs, err = ParseFile("file1.sql", `APPLICATION test();
	ABSTRACT WORKSPACE w1();
		ABSTRACT WORKSPACE w2 INHERITS w1,w2(
		TABLE table4 INHERITS CDoc();
	);
	ABSTRACT WORKSPACE w3 INHERITS w4();
	ABSTRACT WORKSPACE w4 INHERITS w5();
	ABSTRACT WORKSPACE w5 INHERITS w3();
	`)
	require.NoError(err)
	pkg, err = BuildPackageSchema("pkg/test", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})

	require.EqualError(err, strings.Join([]string{
		"file1.sql:3:37: circular reference in INHERITS",
		"file1.sql:6:33: circular reference in INHERITS",
		"file1.sql:7:33: circular reference in INHERITS",
		"file1.sql:8:33: circular reference in INHERITS",
	}, "\n"))
}

func Test_Workspace_Defs(t *testing.T) {

	require := require.New(t)

	fs1, err := ParseFile("file1.sql", `APPLICATION test();
		ABSTRACT WORKSPACE AWorkspace(
			TABLE table1 INHERITS CDoc (a ref);
		);
	`)
	require.NoError(err)
	fs2, err := ParseFile("file2.sql", `
		ALTER WORKSPACE AWorkspace(
			TABLE table2 INHERITS CDoc (a ref);
		);
		WORKSPACE MyWorkspace INHERITS AWorkspace();
		WORKSPACE MyWorkspace2 INHERITS AWorkspace();
		ALTER WORKSPACE sys.Profile(
			USE WORKSPACE MyWorkspace;
		);
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg1", []*FileSchemaAST{fs1, fs2})
	require.NoError(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)
	builder := appdef.New()
	require.NoError(BuildAppDefs(packages, builder))
	ws := builder.Workspace(appdef.NewQName("pkg1", "MyWorkspace"))

	require.Equal(appdef.TypeKind_CDoc, ws.Type(appdef.NewQName("pkg1", "table1")).Kind())
	require.Equal(appdef.TypeKind_CDoc, ws.Type(appdef.NewQName("pkg1", "table2")).Kind())
	require.Equal(appdef.TypeKind_Command, ws.Type(appdef.NewQName("sys", "CreateLogin")).Kind())

	wsProfile := builder.Workspace(appdef.NewQName("sys", "Profile"))

	require.Equal(appdef.TypeKind_Workspace, wsProfile.Type(appdef.NewQName("pkg1", "MyWorkspace")).Kind())
	require.Nil(wsProfile.TypeByName(appdef.NewQName("pkg1", "MyWorkspace2")))

	_, err = builder.Build()
	require.NoError(err)
}

func Test_Alter_Workspace(t *testing.T) {

	require := require.New(t)

	fs0, err := ParseFile("file0.sql", `
	IMPORT SCHEMA 'org/pkg1';
	IMPORT SCHEMA 'org/pkg2';
	APPLICATION test(
		USE pkg1;
		USE pkg2;
	);
	`)
	require.NoError(err)
	pkg0, err := BuildPackageSchema("org/main", []*FileSchemaAST{fs0})
	require.NoError(err)

	fs1, err := ParseFile("file1.sql", `
		ABSTRACT WORKSPACE AWorkspace(
			TABLE table1 INHERITS CDoc (a ref);
		);
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("org/pkg1", []*FileSchemaAST{fs1})
	require.NoError(err)

	fs2, err := ParseFile("file2.sql", `
		IMPORT SCHEMA 'org/pkg1'
		ALTER WORKSPACE pkg1.AWorkspace(
			TABLE table2 INHERITS CDoc (a ref);
		);
	`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("org/pkg2", []*FileSchemaAST{fs2})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg0,
		pkg1,
		pkg2,
	})
	require.EqualError(err, strings.Join([]string{
		"file2.sql:3:19: workspace pkg1.AWorkspace is not alterable",
	}, "\n"))
}

func Test_DupFieldsInTypes(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	TYPE RootType (
		Id int32
	);
	TYPE BaseType(
		RootType,
		baseField int
	);
	TYPE BaseType2 (
		someField int
	);
	TYPE MyType(
		BaseType,
		BaseType2,
		field varchar,
		field varchar,
		baseField varchar,
		someField int,
		Id varchar
	)
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("pkg/test", []*FileSchemaAST{fs})
	require.NoError(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)

	err = BuildAppDefs(packages, appdef.New())
	require.EqualError(err, strings.Join([]string{
		"file1.sql:16:3: redefinition of field",
		"file1.sql:17:3: redefinition of baseField",
		"file1.sql:18:3: redefinition of someField",
		"file1.sql:19:3: redefinition of Id",
	}, "\n"))

}

func Test_Varchar(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", fmt.Sprintf(`APPLICATION test();
	TYPE RootType (
		Oversize varchar(%d)
	);
	TYPE CDoc1 (
		Oversize varchar(%d)
	);
	`, uint32(appdef.MaxFieldLength)+1, uint32(appdef.MaxFieldLength)+1))
	require.NoError(err)
	pkg, err := BuildPackageSchema("pkg/test", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.EqualError(err, strings.Join([]string{
		fmt.Sprintf("file1.sql:3:12: maximum field length is %d", appdef.MaxFieldLength),
		fmt.Sprintf("file1.sql:6:12: maximum field length is %d", appdef.MaxFieldLength),
	}, "\n"))

}

func Test_DupFieldsInTables(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	TYPE RootType (
		Kind int32
	);
	TYPE BaseType(
		RootType,
		baseField int
	);
	TYPE BaseType2 (
		someField int
	);
	ABSTRACT TABLE ByBaseTable INHERITS CDoc (
		Name varchar,
		Code varchar
	);
	TABLE MyTable INHERITS ByBaseTable(
		BaseType,
		BaseType2,
		newField varchar,
		field varchar,
		field varchar, 		-- duplicated in the this table
		baseField varchar,		-- duplicated in the first OF
		someField int,		-- duplicated in the second OF
		Kind int,			-- duplicated in the first OF (2nd level)
		Name int,			-- duplicated in the inherited table
		ID varchar
	)
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("pkg/test", []*FileSchemaAST{fs})
	require.NoError(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)

	err = BuildAppDefs(packages, appdef.New())
	require.EqualError(err, strings.Join([]string{
		"file1.sql:21:3: redefinition of field",
		"file1.sql:22:3: redefinition of baseField",
		"file1.sql:23:3: redefinition of someField",
		"file1.sql:24:3: redefinition of Kind",
		"file1.sql:25:3: redefinition of Name",
	}, "\n"))

}

func Test_AbstractTables(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	TABLE ByBaseTable INHERITS CDoc (
		Name varchar
	);
	TABLE MyTable INHERITS ByBaseTable(		-- NOT ALLOWED (base table must be abstract)
	);

	TABLE My1 INHERITS CRecord(
		f1 ref(AbstractTable)				-- NOT ALLOWED (reference to abstract table)
	);

	ABSTRACT TABLE AbstractTable INHERITS CDoc(
	);

	WORKSPACE MyWorkspace1(
		EXTENSION ENGINE BUILTIN (

			PROJECTOR proj1
            AFTER INSERT ON AbstractTable 	-- NOT ALLOWED (projector refers to abstract table)
            INTENTS(SendMail);

			SYNC PROJECTOR proj2
            AFTER INSERT ON My1
            INTENTS(Record(AbstractTable));	-- NOT ALLOWED (projector refers to abstract table)

			PROJECTOR proj3
            AFTER INSERT ON My1
			STATE(Record(AbstractTable))		-- NOT ALLOWED (projector refers to abstract table)
            INTENTS(SendMail);
		);
		TABLE My2 INHERITS CRecord(
			nested AbstractTable			-- NOT ALLOWED
		);
		USE TABLE AbstractTable;			-- NOT ALLOWED
		TABLE My3 INHERITS CRecord(
			f int,
			items ABSTRACT TABLE Nested()	-- NOT ALLOWED
		);
	)
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.EqualError(err, strings.Join([]string{
		"file1.sql:5:25: base table must be abstract",
		"file1.sql:9:10: reference to abstract table AbstractTable",
		"file1.sql:19:29: projector refers to abstract table AbstractTable",
		"file1.sql:24:21: projector refers to abstract table AbstractTable",
		"file1.sql:28:10: projector refers to abstract table AbstractTable",
		"file1.sql:32:11: nested abstract table AbstractTable",
		"file1.sql:34:13: use of abstract table AbstractTable",
		"file1.sql:37:4: nested abstract table Nested",
	}, "\n"))

}

func Test_AbstractTables2(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	ABSTRACT TABLE AbstractTable INHERITS CDoc(
	);

	WORKSPACE MyWorkspace1(
		TABLE My2 INHERITS CRecord(
			nested AbstractTable			-- NOT ALLOWED
		);
	);
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.EqualError(err, strings.Join([]string{
		"file1.sql:7:11: nested abstract table AbstractTable",
	}, "\n"))

}

func Test_WorkspaceDescriptors(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	ROLE R1;
	WORKSPACE W1(
		DESCRIPTOR(); -- gets name W1Descriptor
	);
	WORKSPACE W2(
		DESCRIPTOR W2D(); -- gets name W2D
	);
	WORKSPACE W3(
		DESCRIPTOR R1(); -- duplicated name
	);
	ROLE W2D; -- duplicated name
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg", []*FileSchemaAST{fs})
	require.EqualError(err, strings.Join([]string{
		"file1.sql:10:14: redefinition of R1",
		"file1.sql:12:2: redefinition of W2D",
	}, "\n"))

	require.Equal(Ident("W1Descriptor"), pkg.Ast.Statements[2].Workspace.Descriptor.Name)
	require.Equal(Ident("W2D"), pkg.Ast.Statements[3].Workspace.Descriptor.Name)
}
func Test_PanicUnknownFieldType(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("file1.sql", `APPLICATION test();
	TABLE MyTable INHERITS CDoc (
		Name asdasd,
		Code varchar
	);
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/pkg", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.EqualError(err, strings.Join([]string{
		"file1.sql:3:8: undefined data type or table: asdasd",
	}, "\n"))

}

func Test_Expressions(t *testing.T) {
	require := require.New(t)

	_, err := ParseFile("file1.sql", `
	TABLE MyTable(
		Int1 varchar DEFAULT 1 CHECK(Int1 > Int2),
		Int1 int DEFAULT 1 CHECK(Text != 'asd'),
		Int1 int DEFAULT 1 CHECK(Int2 > -5),
		Int1 int DEFAULT 1 CHECK(TextField > 'asd' AND (SomeFloat/3.2)*4 != 5.003),
		Int1 int DEFAULT 1 CHECK(SomeFunc('a', TextField) AND BoolField=FALSE),

		CHECK(MyRowValidator(this))
	)
	`)
	require.NoError(err)

}

func Test_Duplicates(t *testing.T) {
	require := require.New(t)

	ast1, err := ParseFile("file1.sql", `APPLICATION test();
	EXTENSION ENGINE BUILTIN (
		FUNCTION MyTableValidator() RETURNS void;
		FUNCTION MyTableValidator(TableRow) RETURNS string;
		FUNCTION MyFunc2() RETURNS void;
	);
	TABLE Rec1 INHERITS CRecord();
	`)
	require.NoError(err)

	ast2, err := ParseFile("file2.sql", `
	WORKSPACE ChildWorkspace (
		TAG MyFunc2; -- redeclared
		EXTENSION ENGINE BUILTIN (
			FUNCTION MyFunc3() RETURNS void;
			FUNCTION MyFunc4() RETURNS void;
		);
		WORKSPACE InnerWorkspace (
			ROLE MyFunc4; -- redeclared
		);
		TABLE Doc1 INHERITS ODoc(
			nested1 Rec1,
			nested2 TABLE Rec1() -- redeclared
		)
	)
	`)
	require.NoError(err)

	_, err = BuildPackageSchema("test/pkg", []*FileSchemaAST{ast1, ast2})

	require.EqualError(err, strings.Join([]string{
		"file1.sql:4:3: redefinition of MyTableValidator",
		"file2.sql:3:3: redefinition of MyFunc2",
		"file2.sql:9:4: redefinition of MyFunc4",
		"file2.sql:13:12: redefinition of Rec1",
	}, "\n"))

}

func Test_DuplicatesInViews(t *testing.T) {
	require := require.New(t)

	ast, err := ParseFile("file2.sql", `APPLICATION test();
	WORKSPACE Workspace (
		VIEW test(
			field1 int,
			field2 int,
			field1 varchar,
			PRIMARY KEY(field1),
			PRIMARY KEY(field2)
		) AS RESULT OF Proj1;

		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`)
	require.NoError(err)

	pkg, err := BuildPackageSchema("test/pkg", []*FileSchemaAST{ast})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		pkg,
		getSysPackageAST(),
	})

	require.EqualError(err, strings.Join([]string{
		"file2.sql:6:4: redefinition of field1",
		"file2.sql:8:16: redefinition of primary key",
	}, "\n"))

}
func Test_Views(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
			VIEW test(
				field1 int,
				PRIMARY KEY(field2)
			) AS RESULT OF Proj1;
			EXTENSION ENGINE BUILTIN (
				PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
				COMMAND Orders()
			);
			)
	`, "file.sql:4:17: undefined field field2")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
			VIEW test(
				field1 varchar,
				PRIMARY KEY((field1))
			) AS RESULT OF Proj1;
			EXTENSION ENGINE BUILTIN (
				PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
				COMMAND Orders()
			);
			)
	`, "file.sql:4:18: varchar field field1 not supported in partition key")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
		VIEW test(
			field1 bytes,
			PRIMARY KEY((field1))
		) AS RESULT OF Proj1;
		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`, "file.sql:4:17: bytes field field1 not supported in partition key")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
		VIEW test(
			field1 varchar,
			field2 int,
			PRIMARY KEY(field1, field2)
		) AS RESULT OF Proj1;
		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`, "file.sql:5:16: varchar field field1 can only be the last one in clustering key")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
		VIEW test(
			field1 bytes,
			field2 int,
			PRIMARY KEY(field1, field2)
		) AS RESULT OF Proj1;
		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`, "file.sql:5:16: bytes field field1 can only be the last one in clustering key")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
		ABSTRACT TABLE abc INHERITS CDoc();
		VIEW test(
			field1 ref(abc),
			field2 ref(unexisting),
			PRIMARY KEY(field1, field2)
		) AS RESULT OF Proj1;
		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`, "file.sql:4:15: reference to abstract table abc", "file.sql:5:15: undefined table: unexisting")

	require.AppSchemaError(`APPLICATION test(); WORKSPACE Workspace (
		VIEW test(
			fld1 int32
		) AS RESULT OF Proj1;
		EXTENSION ENGINE BUILTIN (
			PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
			COMMAND Orders()
		);
	)
	`, "file.sql:2:3: primary key not defined")
}

func Test_Views2(t *testing.T) {
	require := require.New(t)

	{
		ast, err := ParseFile("file2.sql", `APPLICATION test(); WORKSPACE Workspace (
			VIEW test(
				-- comment1
				field1 int,
				-- comment2
				field2 varchar(20),
				-- comment3
				field3 bytes(20),
				-- comment4
				field4 ref,
				PRIMARY KEY((field1,field4),field2)
			) AS RESULT OF Proj1;
			EXTENSION ENGINE BUILTIN (
				PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
				COMMAND Orders()
			);
		)
		`)
		require.NoError(err)
		pkg, err := BuildPackageSchema("test", []*FileSchemaAST{ast})
		require.NoError(err)

		packages, err := BuildAppSchema([]*PackageSchemaAST{
			getSysPackageAST(),
			pkg,
		})
		require.NoError(err)

		appBld := appdef.New()
		err = BuildAppDefs(packages, appBld)
		require.NoError(err)

		v := appBld.View(appdef.NewQName("test", "test"))
		require.NotNil(v)

		_, err = appBld.Build()
		require.NoError(err)
	}
	{
		ast, err := ParseFile("file2.sql", `APPLICATION test(); WORKSPACE Workspace (
			VIEW test(
				-- comment1
				field1 int,
				-- comment2
				field3 bytes(20),
				-- comment4
				field4 ref,
				PRIMARY KEY((field1),field4,field3)
			) AS RESULT OF Proj1;
			EXTENSION ENGINE BUILTIN (
				PROJECTOR Proj1 AFTER EXECUTE ON (Orders) INTENTS (View(test));
				COMMAND Orders()
			);
		)
		`)
		require.NoError(err)
		pkg, err := BuildPackageSchema("test", []*FileSchemaAST{ast})
		require.NoError(err)

		packages, err := BuildAppSchema([]*PackageSchemaAST{
			getSysPackageAST(),
			pkg,
		})
		require.NoError(err)

		appBld := appdef.New()
		err = BuildAppDefs(packages, appBld)
		require.NoError(err)

		v := appBld.View(appdef.NewQName("test", "test"))
		require.NotNil(v)

		_, err = appBld.Build()
		require.NoError(err)
	}
	{
		ast, err := ParseFile("file2.sql", `APPLICATION test(); WORKSPACE Workspace (
			VIEW test(
				-- comment1
				field1 int,
				-- comment2
				field3 bytes(20),
				-- comment4
				field4 ref,
				PRIMARY KEY((field1),field4,field3)
			) AS RESULT OF Proj1;
			EXTENSION ENGINE BUILTIN (
				PROJECTOR Proj1 AFTER EXECUTE ON (Orders);
				COMMAND Orders()
			);
		)
		`)
		require.NoError(err)
		pkg, err := BuildPackageSchema("test", []*FileSchemaAST{ast})
		require.NoError(err)

		_, err = BuildAppSchema([]*PackageSchemaAST{
			getSysPackageAST(),
			pkg,
		})
		require.Error(err, "file2.sql:2:4: projector Proj1 does not declare intent for view test")

	}

}
func Test_Comments(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `
	EXTENSION ENGINE BUILTIN (

	-- My function
	-- line 2
	FUNCTION MyFunc() RETURNS void;

	/* 	Multiline
		comment  */
	FUNCTION MyFunc1() RETURNS void;
	);

	`)
	require.NoError(err)

	ps, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	require.NotNil(ps.Ast.Statements[0].ExtEngine.Statements[0].Function.Comments)

	comments := ps.Ast.Statements[0].ExtEngine.Statements[0].Function.GetComments()
	require.Equal(2, len(comments))
	require.Equal("My function", comments[0])
	require.Equal("line 2", comments[1])

	fn := ps.Ast.Statements[0].ExtEngine.Statements[1].Function
	comments = fn.GetComments()
	require.Equal(2, len(comments))
	require.Equal("Multiline", comments[0])
	require.Equal("comment", comments[1])
}

func Test_Undefined(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	WORKSPACE test (
		EXTENSION ENGINE WASM (
			COMMAND Orders() WITH Tags=(UndefinedTag);
			PROJECTOR ImProjector AFTER EXECUTE ON xyz.CreateUPProfile;
			COMMAND CmdFakeReturn() RETURNS text;
			COMMAND CmdNoReturn() RETURNS void;
			COMMAND CmdFakeArg(text);
			COMMAND CmdVoidArg(void);
			COMMAND CmdFakeUnloggedArg(UNLOGGED text);
		)
	)
	`)
	require.Nil(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{pkg, getSysPackageAST()})

	require.EqualError(err, strings.Join([]string{
		"example.sql:4:32: undefined tag: UndefinedTag",
		"example.sql:5:43: xyz undefined",
		"example.sql:6:36: undefined type or table: text",
		"example.sql:8:23: undefined type or table: text",
		"example.sql:10:40: undefined type or table: text",
	}, "\n"))
}

func Test_Projectors(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	WORKSPACE test (
		TABLE Order INHERITS ODoc();
		EXTENSION ENGINE WASM (
			COMMAND Orders();
			PROJECTOR ImProjector1 AFTER EXECUTE ON test.CreateUPProfile; 			-- Undefined
			PROJECTOR ImProjector2 AFTER EXECUTE ON Order; 							-- Bad: Order is not a type or command
			PROJECTOR ImProjector3 AFTER UPDATE ON Order; 				-- Bad
			PROJECTOR ImProjector4 AFTER ACTIVATE ON Order; 			-- Bad
			PROJECTOR ImProjector5 AFTER DEACTIVATE ON Order; 			-- Bad
			PROJECTOR ImProjector6 AFTER INSERT ON Order OR AFTER EXECUTE ON Orders;	-- Good
			PROJECTOR ImProjector7 AFTER EXECUTE WITH PARAM ON Bill;	-- Bad: Type undefined
			PROJECTOR ImProjector8 AFTER EXECUTE WITH PARAM ON ODoc;	-- Good
			PROJECTOR ImProjector9 AFTER EXECUTE WITH PARAM ON ORecord;	-- Bad
		);
	)
	`)
	require.Nil(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{pkg, getSysPackageAST()})

	require.EqualError(err, strings.Join([]string{
		"example.sql:6:44: undefined command: test.CreateUPProfile",
		"example.sql:7:44: undefined command: Order",
		"example.sql:8:43: only INSERT allowed for ODoc or ORecord",
		"example.sql:9:45: only INSERT allowed for ODoc or ORecord",
		"example.sql:10:47: only INSERT allowed for ODoc or ORecord",
		"example.sql:12:55: undefined type or ODoc: Bill",
		"example.sql:14:55: undefined type or ODoc: ORecord",
	}, "\n"))
}

func Test_Imports(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2';
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3' AS air;
	APPLICATION test(
		USE pkg2;
		USE pkg3;
	);
	WORKSPACE test (
		EXTENSION ENGINE WASM (
    		COMMAND Orders WITH Tags=(pkg2.SomeTag);
    		QUERY Query2 RETURNS void WITH Tags=(air.SomePkg3Tag);
    		QUERY Query3 RETURNS void WITH Tags=(air.UnknownTag); -- air.UnknownTag undefined
    		PROJECTOR ImProjector AFTER EXECUTE ON Air.CreateUPProfil; -- Air undefined
		)
	)
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example.sql", `TAG SomeTag;`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example.sql", `TAG SomePkg3Tag;`)
	require.NoError(err)
	pkg3, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg3", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{getSysPackageAST(), pkg1, pkg2, pkg3})
	require.EqualError(err, strings.Join([]string{
		"example.sql:12:44: undefined tag: air.UnknownTag",
		"example.sql:13:46: Air undefined",
	}, "\n"))

}

func Test_AbstractWorkspace(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	WORKSPACE ws1 ();
	ABSTRACT WORKSPACE ws2(
		DESCRIPTOR(					-- Incorrect
			a int
		);
	);
	WORKSPACE ws4 INHERITS ws2 ();
	WORKSPACE ws5 INHERITS ws1 ();  -- Incorrect
	`)
	require.Nil(err)

	ps, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	require.False(ps.Ast.Statements[1].Workspace.Abstract)
	require.True(ps.Ast.Statements[2].Workspace.Abstract)
	require.False(ps.Ast.Statements[3].Workspace.Abstract)
	require.Equal("ws2", ps.Ast.Statements[3].Workspace.Inherits[0].String())

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		ps,
	})
	require.EqualError(err, strings.Join([]string{
		"example.sql:4:13: abstract workspace cannot have a descriptor",
		"example.sql:9:25: base workspace must be abstract",
	}, "\n"))

}

func Test_UniqueFields(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	TABLE MyTable INHERITS CDoc (
		Int1 int32,
		Int2 int32 NOT NULL,
		UNIQUEFIELD Int1,
		UNIQUEFIELD Int2
	)
	`)
	require.NoError(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)

	appBld := appdef.New()
	err = BuildAppDefs(packages, appBld)
	require.NoError(err)

	cdoc := appBld.CDoc(appdef.NewQName("test", "MyTable"))
	require.NotNil(cdoc)

	fld := cdoc.UniqueField()
	require.NotNil(fld)
	require.Equal("Int2", fld.Name())

	_, err = appBld.Build()
	require.NoError(err)

}

func Test_NestedTables(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	TABLE NestedTable INHERITS CRecord (
		ItemName varchar,
		DeepNested TABLE DeepNestedTable (
			ItemName varchar
		)
	);
	`)
	require.Nil(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)

	appBld := appdef.New()
	err = BuildAppDefs(packages, appBld)
	require.NoError(err)

	require.NotNil(appBld.CRecord(appdef.NewQName("test", "NestedTable")))
	require.NotNil(appBld.CRecord(appdef.NewQName("test", "DeepNestedTable")))
	_, err = appBld.Build()
	require.NoError(err)
}

func Test_SemanticAnalysisForReferences(t *testing.T) {
	t.Run("Should return error because CDoc references to ODoc", func(t *testing.T) {
		require := require.New(t)

		fs, err := ParseFile("example.sql", `APPLICATION test();
		TABLE OTable INHERITS ODoc ();
		TABLE CTable INHERITS CDoc (
			OTableRef ref(OTable)
		);
		`)
		require.Nil(err)

		pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
		require.Nil(err)

		packages, err := BuildAppSchema([]*PackageSchemaAST{
			getSysPackageAST(),
			pkg,
		})
		require.NoError(err)

		appBld := appdef.New()
		err = BuildAppDefs(packages, appBld)

		require.Contains(err.Error(), "table test.CTable can not reference to table test.OTable")
	})
}

func Test_1KStringField(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	TABLE MyTable INHERITS CDoc (
		KB varchar(1024)
	)
	`)
	require.Nil(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.NoError(err)

	appBld := appdef.New()
	err = BuildAppDefs(packages, appBld)
	require.NoError(err)

	cdoc := appBld.CDoc(appdef.NewQName("test", "MyTable"))
	require.NotNil(cdoc)

	fld := cdoc.Field("KB")
	require.NotNil(fld)

	cnt := 0
	for _, c := range fld.Constraints() {
		cnt++
		require.Equal(appdef.ConstraintKind_MaxLen, c.Kind())
		require.EqualValues(1024, c.Value())
	}
	require.Equal(1, cnt)

	_, err = appBld.Build()
	require.NoError(err)

}

func Test_ReferenceToNoTable(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example.sql", `APPLICATION test();
	ROLE Admin;
	TABLE CTable INHERITS CDoc (
		RefField ref(Admin)
	);
	`)
	require.Nil(err)

	pkg, err := BuildPackageSchema("test", []*FileSchemaAST{fs})
	require.Nil(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
	})
	require.Contains(err.Error(), "undefined table: Admin")

}

func Test_VRestaurantBasic(t *testing.T) {

	require := require.New(t)

	vRestaurantPkgAST, err := ParsePackageDir("github.com/untillpro/vrestaurant", fsvRestaurant, "sql_example_app/vrestaurant")
	require.NoError(err)

	packages, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		vRestaurantPkgAST,
	})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(packages, builder)
	require.NoError(err)

	// table
	cdoc := builder.Type(appdef.NewQName("vrestaurant", "TablePlan"))
	require.NotNil(cdoc)
	require.Equal(appdef.TypeKind_CDoc, cdoc.Kind())
	require.Equal(appdef.DataKind_RecordID, cdoc.(appdef.IFields).Field("Picture").DataKind())

	cdoc = builder.Type(appdef.NewQName("vrestaurant", "Client"))
	require.NotNil(cdoc)

	cdoc = builder.Type(appdef.NewQName("vrestaurant", "POSUser"))
	require.NotNil(cdoc)

	cdoc = builder.Type(appdef.NewQName("vrestaurant", "Department"))
	require.NotNil(cdoc)

	cdoc = builder.Type(appdef.NewQName("vrestaurant", "Article"))
	require.NotNil(cdoc)

	// child table
	crec := builder.Type(appdef.NewQName("vrestaurant", "TableItem"))
	require.NotNil(crec)
	require.Equal(appdef.TypeKind_CRecord, crec.Kind())
	require.Equal(appdef.DataKind_int32, crec.(appdef.IFields).Field("Tableno").DataKind())

	// view
	view := builder.View(appdef.NewQName("vrestaurant", "SalesPerDay"))
	require.NotNil(view)
	require.Equal(appdef.TypeKind_ViewRecord, view.Kind())

	_, err = builder.Build()
	require.NoError(err)

}

func Test_AppSchema(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("example1.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2' AS air1;
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3' AS air2;
	APPLICATION test(
		USE air1;
		USE air2;
	);
	TABLE MyTable INHERITS CDoc ();
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example2.sql", `
	TABLE MyTable INHERITS CDoc ();
	`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example3.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2' AS p2;
	WORKSPACE myWorkspace (
		USE TABLE p2.MyTable;
	);
	`)
	require.NoError(err)
	pkg3, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg3", []*FileSchemaAST{fs})
	require.NoError(err)

	appSchema, err := BuildAppSchema([]*PackageSchemaAST{getSysPackageAST(), pkg1, pkg2, pkg3})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(appSchema, builder)
	require.NoError(err)

	cdoc := builder.CDoc(appdef.NewQName("pkg1", "MyTable"))
	require.NotNil(cdoc)

	cdoc = builder.CDoc(appdef.NewQName("air1", "MyTable"))
	require.NotNil(cdoc)

	ws := builder.Workspace(appdef.NewQName("air2", "myWorkspace"))
	require.NotNil(ws)
	require.NotNil(ws.Type(appdef.NewQName("air1", "MyTable")))

	_, err = builder.Build()
	require.NoError(err)
}

func Test_AppSchemaErrors(t *testing.T) {
	require := require.New(t)
	fs, err := ParseFile("example2.sql", ``)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example3.sql", ``)
	require.NoError(err)
	pkg3, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg3", []*FileSchemaAST{fs})
	require.NoError(err)

	f := func(sql string, expectErrors ...string) {
		ast, err := ParseFile("file2.sql", sql)
		require.NoError(err)
		pkg, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg4", []*FileSchemaAST{ast})
		require.NoError(err)

		_, err = BuildAppSchema([]*PackageSchemaAST{
			pkg, pkg2, pkg3,
		})
		require.EqualError(err, strings.Join(expectErrors, "\n"))
	}

	f(`IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3';
	APPLICATION test(
		USE air1;
		USE pkg3;
		)`, "file2.sql:3:3: air1 undefined",
		"application does not define use of package github.com/untillpro/airsbp3/pkg2")

	f(`IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2' AS air1;
		IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3';
		APPLICATION test(
			USE air1;
			USE pkg3;
			USE pkg3;
		)`, "file2.sql:6:4: package with the same name already included in application")

	f(`IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2' AS air1;
		IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3';
		APPLICATION test(
			USE air1;
			USE pkg3;
		);
		APPLICATION test(
			USE air1;
			USE pkg3;
		)`, "file2.sql:7:3: redefinition of application")

	f(`IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2' AS air1;
		IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3';
		`, "application not defined")

	f(`IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkgX' AS air1;
		IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg3';
		APPLICATION test(
			USE pkg3;
			USE air1;
		)
		`, "file2.sql:5:4: could not import github.com/untillpro/airsbp3/pkgX")
}

func Test_AppIn2Schemas(t *testing.T) {
	require := require.New(t)
	fs, err := ParseFile("example2.sql", `APPLICATION test1();`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	fs, err = ParseFile("example3.sql", `APPLICATION test2();`)
	require.NoError(err)
	pkg3, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg3", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		pkg2, pkg3,
	})
	require.ErrorContains(err, "redefinition of application")
}

func Test_Scope(t *testing.T) {
	require := require.New(t)

	// *****  main
	fs, err := ParseFile("example1.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg1';
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg2';
	APPLICATION test(
		USE pkg1;
		USE pkg2;
	);
	`)
	require.NoError(err)
	main, err := BuildPackageSchema("github.com/untillpro/airsbp3/main", []*FileSchemaAST{fs})
	require.NoError(err)

	// *****  pkg1
	fs, err = ParseFile("example2.sql", `
	WORKSPACE myWorkspace1 (
		TABLE MyTable INHERITS CDoc ();
	);
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)

	// *****  pkg2
	fs, err = ParseFile("example3.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg1' AS p1;
	WORKSPACE myWorkspace2 (
		USE TABLE p1.MyTable;
	);
	`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{getSysPackageAST(), main, pkg1, pkg2})
	require.EqualError(err, "example3.sql:4:16: undefined table: p1.MyTable")

}

func Test_Scope_TableRefs(t *testing.T) {
	require := require.New(t)

	// *****  main
	fs, err := ParseFile("example1.sql", `
	IMPORT SCHEMA 'github.com/untillpro/airsbp3/pkg1';
	APPLICATION test(
		USE pkg1;
	);
	`)
	require.NoError(err)
	main, err := BuildPackageSchema("github.com/untillpro/airsbp3/main", []*FileSchemaAST{fs})
	require.NoError(err)

	// *****  pkg1
	fs, err = ParseFile("example2.sql", `
	TABLE PkgTable INHERITS CRecord();
	WORKSPACE myWorkspace1 (
		TABLE MyTable INHERITS CDoc (
			Items TABLE MyInnerTable()
		);
		TABLE MyTable2 INHERITS CDoc (
			r1 ref(MyTable),
			r2 ref(MyTable2),
			r3 ref(PkgTable),
			r4 ref(MyInnerTable)
		);
	);
	WORKSPACE myWorkspace2 (
		TABLE MyTable3 INHERITS CDoc (
			r1 ref(MyTable),
			r2 ref(MyTable2),
			r3 ref(PkgTable),
			r4 ref(MyInnerTable)
		);
	);
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg1", []*FileSchemaAST{fs})
	require.NoError(err)
	_, err = BuildAppSchema([]*PackageSchemaAST{getSysPackageAST(), main, pkg1})
	require.EqualError(err, strings.Join([]string{
		"example2.sql:16:11: undefined table: MyTable",
		"example2.sql:17:11: undefined table: MyTable2",
		"example2.sql:19:11: undefined table: MyInnerTable",
	}, "\n"))

}

func Test_Alter_Workspace_In_Package(t *testing.T) {

	require := require.New(t)

	fs0, err := ParseFile("file0.sql", `
	IMPORT SCHEMA 'org/pkg1';
	IMPORT SCHEMA 'org/pkg2';
	APPLICATION test(
		USE pkg1;
	);
	`)
	require.NoError(err)
	pkg0, err := BuildPackageSchema("org/main", []*FileSchemaAST{fs0})
	require.NoError(err)

	fs1, err := ParseFile("file1.sql", `
		ALTERABLE WORKSPACE _Ws(
			TABLE _wst1 INHERITS CDoc();
		);
		ABSTRACT WORKSPACE AWs(
			TABLE awst1 INHERITS CDoc();
		);
		WORKSPACE Ws(
			TABLE wst1 INHERITS CDoc();
		);
	`)
	require.NoError(err)
	fs2, err := ParseFile("file2.sql", `
		ALTER WORKSPACE _Ws(
			TABLE _wst2 INHERITS CDoc();
		);
		ALTER WORKSPACE AWs(
			TABLE awst2 INHERITS CDoc();
		);
		ALTER WORKSPACE Ws(
			TABLE wst2 INHERITS CDoc();
		);
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("org/pkg1", []*FileSchemaAST{fs1, fs2})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg0,
		pkg1,
	})
	require.NoError(err)
}

func Test_UseTableErrors(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("main.sql", `
	IMPORT SCHEMA 'org/pkg1';
	APPLICATION test(
		USE pkg1;
	);
	WORKSPACE Ws(
		USE TABLE pkg1.Pkg1Table3;  -- bad, declared in workspace
		USE TABLE pkg2.*;  			-- bad, package not found
		USE WORKSPACE ws1;			-- bad, workspace not found
	)
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/main", []*FileSchemaAST{fs})
	require.NoError(err)

	// pkg1
	fs1, err := ParseFile("file1.sql", `
	WORKSPACE Ws(
		TABLE Pkg1Table3 INHERITS CDoc();
	)
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("org/pkg1", []*FileSchemaAST{fs1})
	require.NoError(err)

	_, err = BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
		pkg1,
	})

	require.EqualError(err, strings.Join([]string{
		"main.sql:7:18: undefined table: pkg1.Pkg1Table3",
		"main.sql:8:13: pkg2 undefined",
		"main.sql:9:17: undefined workspace: main.ws1",
	}, "\n"))
}

func Test_UseTables(t *testing.T) {
	require := require.New(t)

	fs, err := ParseFile("main.sql", `
	IMPORT SCHEMA 'org/pkg1';
	IMPORT SCHEMA 'org/pkg2';
	APPLICATION test(
		USE pkg1;
		USE pkg2;
	);
	TABLE TestTable1 INHERITS CDoc();
	TABLE TestTable2 INHERITS CDoc();

	WORKSPACE Ws(
		USE TABLE *;				-- good, import all tables declared on current package level
		USE TABLE pkg1.*;			-- good, import all tables from specified package
		USE TABLE pkg2.Pkg2Table1;	-- good, import specified table
	)
	`)
	require.NoError(err)
	pkg, err := BuildPackageSchema("test/main", []*FileSchemaAST{fs})
	require.NoError(err)

	// pkg1
	fs1, err := ParseFile("file1.sql", `
	TABLE Pkg1Table1 INHERITS CDoc();
	TABLE Pkg1Table2 INHERITS CDoc();

	WORKSPACE Ws(
		TABLE Pkg1Table3 INHERITS CDoc();
	)
	`)
	require.NoError(err)
	pkg1, err := BuildPackageSchema("org/pkg1", []*FileSchemaAST{fs1})
	require.NoError(err)

	// pkg2
	fs2, err := ParseFile("file2.sql", `
	TABLE Pkg2Table1 INHERITS CDoc();
	TABLE Pkg2Table2 INHERITS CDoc();

	WORKSPACE Ws(
		TABLE Pkg2Table3 INHERITS CDoc();
	)
	`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("org/pkg2", []*FileSchemaAST{fs2})
	require.NoError(err)

	schema, err := BuildAppSchema([]*PackageSchemaAST{
		getSysPackageAST(),
		pkg,
		pkg1,
		pkg2,
	})

	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(schema, builder)
	require.NoError(err)

	ws := builder.Workspace(appdef.NewQName("main", "Ws"))
	require.NotNil(ws)

	require.NotEqual(appdef.TypeKind_null, ws.Type(appdef.NewQName("main", "TestTable1")).Kind())
	require.NotEqual(appdef.TypeKind_null, ws.Type(appdef.NewQName("main", "TestTable2")).Kind())
	require.NotEqual(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg1", "Pkg1Table1")).Kind())
	require.NotEqual(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg1", "Pkg1Table2")).Kind())
	require.Equal(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg1", "Pkg1Table3")).Kind())
	require.NotEqual(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg2", "Pkg2Table1")).Kind())
	require.Equal(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg2", "Pkg2Table2")).Kind())
	require.Equal(appdef.TypeKind_null, ws.Type(appdef.NewQName("pkg2", "Pkg2Table3")).Kind())

	_, err = builder.Build()
	require.NoError(err)

}

func Test_Storages(t *testing.T) {
	require := require.New(t)
	fs, err := ParseFile("example2.sql", `APPLICATION test1();
	EXTENSION ENGINE BUILTIN (
		STORAGE MyStorage(
			INSERT SCOPE(PROJECTORS)
		);
	)
	`)
	require.NoError(err)
	pkg2, err := BuildPackageSchema("github.com/untillpro/airsbp3/pkg2", []*FileSchemaAST{fs})
	require.NoError(err)

	schema, err := BuildAppSchema([]*PackageSchemaAST{
		pkg2,
	})
	require.ErrorContains(err, "storages are only declared in sys package")
	require.Nil(schema)
}

func buildPackage(sql string) *PackageSchemaAST {
	fs, err := ParseFile("source.sql", sql)
	if err != nil {
		panic(err)
	}
	pkg, err := BuildPackageSchema("github.com/voedger/voedger/app1", []*FileSchemaAST{fs})
	if err != nil {
		panic(err)
	}
	return pkg
}

func Test_OdocCmdArgs(t *testing.T) {
	require := require.New(t)
	pkgApp1 := buildPackage(`

	APPLICATION registry(
	);

	TABLE TableODoc INHERITS ODoc (
		orecord1 TABLE orecord1(
			orecord2 TABLE orecord2()
		)
	);

	WORKSPACE Workspace1 (
		EXTENSION ENGINE BUILTIN (
			COMMAND CmdODoc1(TableODoc) RETURNS TableODoc;
		)
	);

	`)

	app, err := BuildAppSchema([]*PackageSchemaAST{pkgApp1, getSysPackageAST()})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(app, builder)
	require.NoError(err)

	_, err = builder.Build()
	require.NoError(err)

	cmdOdoc := builder.Command(appdef.NewQName("app1", "CmdODoc1"))
	require.NotNil(cmdOdoc)
	require.NotNil(cmdOdoc.Param())

	odoc := cmdOdoc.Param().(appdef.IContainers)
	require.Equal(1, odoc.ContainerCount())
	require.Equal("orecord1", odoc.Container("orecord1").Name())
	container := odoc.Container("orecord1")
	require.Equal(appdef.Occurs(0), container.MinOccurs())
	require.Equal(appdef.Occurs(100), container.MaxOccurs())

	orec := builder.ORecord(appdef.NewQName("app1", "orecord1"))
	require.NotNil(orec)
	require.Equal(1, orec.ContainerCount())
	require.Equal("orecord2", orec.Container("orecord2").Name())

}

func Test_TypeContainers(t *testing.T) {
	require := require.New(t)
	pkgApp1 := buildPackage(`

APPLICATION registry(
);

TYPE Person (
	Name 	varchar,
	Age 	int32
);

TYPE Item (
	Name 	varchar,
	Price 	currency
);

TYPE Deal (
	side1 		Person NOT NULL,	-- collection 1..1
	side2 		Person				-- collection 0..1
--	items 		Item[] NOT NULL,	-- (not yet supported by kernel) collection 1..* (up to maxNestedTableContainerOccurrences = 100)
--	discounts 	Item[3]				-- (not yet supported by kernel) collection 0..3 (one-based numbering convention for arrays, similarly to PostgreSQL)
);

WORKSPACE Workspace1 (
	EXTENSION ENGINE BUILTIN (
		COMMAND CmdDeal(Deal) RETURNS Deal;
	)
);
	`)

	app, err := BuildAppSchema([]*PackageSchemaAST{pkgApp1, getSysPackageAST()})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(app, builder)
	require.NoError(err)

	validate := func(par appdef.IType) {
		o, ok := par.(appdef.IObject)
		require.True(ok, "expected %v supports IObject", par)
		require.Equal(2, o.ContainerCount())
		require.Equal(appdef.Occurs(1), o.Container("side1").MinOccurs())
		require.Equal(appdef.Occurs(1), o.Container("side1").MaxOccurs())
		require.Equal(appdef.Occurs(0), o.Container("side2").MinOccurs())
		require.Equal(appdef.Occurs(1), o.Container("side2").MaxOccurs())

		/* TODO: uncomment when kernel supports it
		require.Equal(appdef.Occurs(1), o.Container("items").MinOccurs())
		require.Equal(appdef.Occurs(100), o.Container("items").MaxOccurs())
		require.Equal(appdef.Occurs(0), o.Container("discounts").MinOccurs())
		require.Equal(appdef.Occurs(3), o.Container("discounts").MaxOccurs())
		*/
	}

	cmd := builder.Command(appdef.NewQName("app1", "CmdDeal"))
	validate(cmd.Param())
	validate(cmd.Result())

	_, err = builder.Build()
	require.NoError(err)

}

func Test_EmptyType(t *testing.T) {
	require := require.New(t)
	pkgApp1 := buildPackage(`

APPLICATION registry(
);

TYPE EmptyType (
);
	`)

	app, err := BuildAppSchema([]*PackageSchemaAST{pkgApp1, getSysPackageAST()})
	require.NoError(err)

	builder := appdef.New()
	err = BuildAppDefs(app, builder)
	require.NoError(err)

	cdoc := builder.Object(appdef.NewQName("app1", "EmptyType"))
	require.NotNil(cdoc)

	_, err = builder.Build()
	require.NoError(err)
}

func Test_EmptyType1(t *testing.T) {
	require := require.New(t)
	pkgApp1 := buildPackage(`

APPLICATION registry(
);

TYPE SomeType (
	t int321
);

TABLE SomeTable INHERITS CDoc (
	t int321
)
	`)

	_, err := BuildAppSchema([]*PackageSchemaAST{pkgApp1, getSysPackageAST()})
	require.EqualError(err, strings.Join([]string{
		"source.sql:7:4: undefined type: int321",
		"source.sql:11:4: undefined data type or table: int321",
	}, "\n"))

}

func Test_ODocUnknown(t *testing.T) {
	require := require.New(t)
	pkgApp1 := buildPackage(`APPLICATION registry();
TABLE MyTable1 INHERITS ODocUnknown ( MyField ref(registry.Login) NOT NULL );
`)

	_, err := BuildAppSchema([]*PackageSchemaAST{pkgApp1, getSysPackageAST()})
	require.EqualError(err, strings.Join([]string{
		"source.sql:2:1: undefined table kind",
		"source.sql:2:51: registry undefined",
		"source.sql:2:25: undefined table: ODocUnknown",
	}, "\n"))

}

//go:embed package.sql
var pkgSqlFS embed.FS

func TestParseFilesFromFSRoot(t *testing.T) {
	t.Run("dot", func(t *testing.T) {
		_, err := ParsePackageDir("github.com/untillpro/main", pkgSqlFS, ".")
		require.NoError(t, err)
	})
}

func Test_Constraints(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`
	APPLICATION app1();
	TABLE SomeTable INHERITS CDoc (
		t1 int32,
		t2 int32,
		CONSTRAINT c1 UNIQUE(t1),
		CONSTRAINT c1 UNIQUE(t2)
	)`, "file.sql:7:3: redefinition of c1")

	require.AppSchemaError(`
	APPLICATION app1();
	TABLE SomeTable INHERITS CDoc (
		UNIQUEFIELD UnknownField
	)`, "file.sql:4:3: undefined field UnknownField")

	require.AppSchemaError(`
	APPLICATION app1();
	TABLE SomeTable INHERITS CDoc (
		t1 int32,
		t2 int32,
		CONSTRAINT c1 UNIQUE(t1),
		CONSTRAINT c2 UNIQUE(t2, t1)
	)`, "file.sql:7:3: field t1 already in unique constraint")

}

func Test_Grants(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`
	APPLICATION app1();
	ROLE role1;
	WORKSPACE ws1 (
		GRANT ALL ON TABLE Fake TO app1;
		GRANT INSERT ON COMMAND Fake TO role1;
		GRANT SELECT ON QUERY Fake TO role1;
		GRANT INSERT ON WORKSPACE Fake TO role1;
		TABLE Tbl INHERITS CDoc();
		GRANT ALL(FakeCol) ON TABLE Tbl TO role1;
		GRANT INSERT,UPDATE(FakeCol) ON TABLE Tbl TO role1;
		GRANT INSERT ON ALL COMMANDS WITH TAG x TO role1;
		TABLE Nested1 INHERITS CRecord();
		TABLE Tbl2 INHERITS CDoc(
			ref1 ref(Tbl),
			items TABLE Nested(),
			items2 Nested1
		);
		GRANT ALL(ref1) ON TABLE Tbl2 TO role1;
		GRANT ALL(items) ON TABLE Tbl2 TO role1;
		GRANT ALL(items2) ON TABLE Tbl2 TO role1;
	);
	`, "file.sql:5:30: undefined role: app1",
		"file.sql:5:22: undefined table: Fake",
		"file.sql:6:27: undefined command: Fake",
		"file.sql:7:25: undefined query: Fake",
		"file.sql:8:29: undefined workspace: Fake",
		"file.sql:10:13: undefined field FakeCol",
		"file.sql:11:23: undefined field FakeCol",
		"file.sql:12:41: undefined tag: x",
	)
}

func Test_UndefinedType(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`APPLICATION app1();
TABLE MyTable2 INHERITS ODoc (
MyField int23 NOT NULL
);
	`, "file.sql:3:9: undefined data type or table: int23",
	)
}

func Test_DescriptorInProjector(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`APPLICATION app1();
	WORKSPACE w (
		EXTENSION ENGINE BUILTIN (
		  PROJECTOR x AFTER INSERT ON (unknown.z) STATE(Http);
		);
	  );
	`,
		"file.sql:4:34: unknown undefined")

	require.NoAppSchemaError(`APPLICATION app1();
	WORKSPACE RestaurantWS (
		DESCRIPTOR Restaurant ();
		EXTENSION ENGINE BUILTIN (
		  PROJECTOR NewRestaurantVat AFTER INSERT OR UPDATE ON (Restaurant) STATE(AppSecret, Http) INTENTS(SendMail);
		);
	  );
	`)
}

type testVarResolver struct {
	resolved map[appdef.QName]bool
}

func (t testVarResolver) AsInt32(name appdef.QName) (int32, bool) {
	t.resolved[name] = true
	return 1, true
}

func Test_Variables(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`APPLICATION app1(); RATE AppDefaultRate variable PER HOUR;`, "file.sql:1:41: variable undefined")

	schema, err := require.AppSchema(`APPLICATION app1();
	DECLARE variable int32 DEFAULT 100;
	RATE AppDefaultRate variable PER HOUR;
	`)
	require.NoError(err)

	resolver := testVarResolver{resolved: make(map[appdef.QName]bool)}

	BuildAppDefs(schema, appdef.New(), WithVariableResolver(&resolver))
	require.True(resolver.resolved[appdef.NewQName("pkg", "variable")])
}

func Test_RatesAndLimits(t *testing.T) {
	require := assertions(t)

	require.AppSchemaError(`APPLICATION app1();
	WORKSPACE w (
		RATE r 1 PER HOUR;
		LIMIT l1 ON EVERYTHING WITH RATE x;
		LIMIT l2 ON COMMAND x WITH RATE r;
		LIMIT l3 ON QUERY y WITH RATE r;
		LIMIT l4 ON TAG z WITH RATE r;
		LIMIT l5 ON TABLE t WITH RATE r;
	);`,
		"file.sql:4:36: undefined rate: x",
		"file.sql:5:23: undefined command: x",
		"file.sql:6:21: undefined query: y",
		"file.sql:7:19: undefined tag: z",
		"file.sql:8:21: undefined table: t")
}
