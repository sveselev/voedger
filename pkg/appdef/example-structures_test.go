/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 */

package appdef_test

import (
	"fmt"

	"github.com/voedger/voedger/pkg/appdef"
)

func ExampleIAppDef_Structures() {

	var app appdef.IAppDef
	docName, recName := appdef.NewQName("test", "document"), appdef.NewQName("test", "record")
	objName, childName := appdef.NewQName("test", "object"), appdef.NewQName("test", "child")

	// how to build AppDef with structures
	{
		appDef := appdef.New()

		doc := appDef.AddCDoc(docName)
		doc.
			AddField("f1", appdef.DataKind_int64, true).
			AddField("f2", appdef.DataKind_string, false)
		doc.AddContainer("rec", recName, 0, appdef.Occurs_Unbounded)

		rec := appDef.AddCRecord(recName)
		rec.
			AddField("f1", appdef.DataKind_int64, true).
			AddField("f2", appdef.DataKind_string, false)

		obj := appDef.AddObject(objName)
		obj.
			AddField("f1", appdef.DataKind_int64, true).
			AddField("f2", appdef.DataKind_string, false)
		obj.AddContainer("child", childName, 0, appdef.Occurs_Unbounded)

		child := appDef.AddObject(childName)
		child.
			AddField("f1", appdef.DataKind_int64, true).
			AddField("f2", appdef.DataKind_string, false)

		if a, err := appDef.Build(); err == nil {
			app = a
		} else {
			panic(err)
		}
	}

	// how to inspect builded AppDef with structures
	{
		cnt := 0
		app.Structures(func(s appdef.IStructure) {
			cnt++
			fmt.Printf("%d. %v\n", cnt, s)
			fmt.Printf("- user/overall field count: %d/%d\n", s.UserFieldCount(), s.FieldCount())
			fmt.Printf("- container count: %d\n", s.ContainerCount())
		})

		fmt.Printf("Overall %d structures\n", cnt)
	}

	// how to inspect builded AppDef with records
	{
		cnt := 0
		app.Records(func(s appdef.IRecord) {
			cnt++
			fmt.Printf("%d. %v\n", cnt, s)
			fmt.Printf("- user/overall field count: %d/%d\n", s.UserFieldCount(), s.FieldCount())
			fmt.Printf("- container count: %d\n", s.ContainerCount())
		})

		fmt.Printf("Overall %d records\n", cnt)

		fmt.Println(app.Record(recName), app.Record(appdef.NewQName("test", "unknown")))
	}

	// Output:
	// 1. Object «test.child»
	// - user/overall field count: 2/4
	// - container count: 0
	// 2. CDoc «test.document»
	// - user/overall field count: 2/5
	// - container count: 1
	// 3. Object «test.object»
	// - user/overall field count: 2/4
	// - container count: 1
	// 4. CRecord «test.record»
	// - user/overall field count: 2/7
	// - container count: 0
	// Overall 4 structures
	// 1. CDoc «test.document»
	// - user/overall field count: 2/5
	// - container count: 1
	// 2. CRecord «test.record»
	// - user/overall field count: 2/7
	// - container count: 0
	// Overall 2 records
	// CRecord «test.record» <nil>
}
