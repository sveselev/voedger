/*
 * Copyright (c) 2023-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_IsSysField(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "true if sys.QName",
			args: args{SystemField_QName},
			want: true,
		},
		{
			name: "true if sys.ID",
			args: args{SystemField_ID},
			want: true,
		},
		{
			name: "true if sys.ParentID",
			args: args{SystemField_ParentID},
			want: true,
		},
		{
			name: "true if sys.Container",
			args: args{SystemField_Container},
			want: true,
		},
		{
			name: "true if sys.IsActive",
			args: args{SystemField_IsActive},
			want: true,
		},
		{
			name: "false if empty",
			args: args{""},
			want: false,
		},
		{
			name: "false if basic user",
			args: args{"userField"},
			want: false,
		},
		{
			name: "false if curious user",
			args: args{"sys.user"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSysField(tt.args.name); got != tt.want {
				t.Errorf("sysField() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_AddField(t *testing.T) {
	require := require.New(t)

	obj := New().AddObject(NewQName("test", "object"))
	require.NotNil(obj)

	t.Run("must be ok to add field", func(t *testing.T) {
		obj.AddField("f1", DataKind_int64, true)

		require.Equal(1, obj.UserFieldCount())
		require.Equal(obj.UserFieldCount()+2, obj.FieldCount()) // + sys.QName + sys.Container

		f := obj.Field("f1")
		require.NotNil(f)
		require.Equal("f1", f.Name())
		require.False(f.IsSys())

		require.Equal(DataKind_int64, f.DataKind())
		require.True(f.IsFixedWidth())
		require.True(f.DataKind().IsFixed())

		require.True(f.Required())
		require.False(f.Verifiable())
	})

	t.Run("chain notation is ok to add fields", func(t *testing.T) {
		o := New().AddObject(NewQName("test", "obj"))
		n := o.AddField("f1", DataKind_int64, true).
			AddField("f2", DataKind_int32, false).
			AddField("f3", DataKind_string, false).(IType).QName()
		require.Equal(o.QName(), n)
		require.Equal(3, o.UserFieldCount())
	})

	t.Run("must be panic if empty field name", func(t *testing.T) {
		require.Panics(func() { obj.AddField("", DataKind_int64, true) })
	})

	t.Run("must be panic if invalid field name", func(t *testing.T) {
		require.Panics(func() { obj.AddField("naked_🔫", DataKind_int64, true) })
	})

	t.Run("must be panic if field name dupe", func(t *testing.T) {
		require.Panics(func() { obj.AddField("f1", DataKind_int64, true) })
	})

	t.Run("must be panic if field data kind is not allowed by type kind", func(t *testing.T) {
		o := New().AddObject(NewQName("test", "object"))
		require.Panics(func() { o.AddField("f1", DataKind_Event, false) })
	})

	t.Run("must be panic if too many fields", func(t *testing.T) {
		o := New().AddObject(NewQName("test", "obj"))
		for i := 0; o.FieldCount() < MaxTypeFieldCount; i++ {
			o.AddField(fmt.Sprintf("f_%#x", i), DataKind_bool, false)
		}
		require.Panics(func() { o.AddField("errorField", DataKind_bool, true) })
	})

	t.Run("must be panic if unsupported field data kind", func(t *testing.T) {
		o := New().AddObject(NewQName("test", "obj"))
		require.Panics(func() { o.AddField("errorField", DataKind_FakeLast, false) })
	})

	t.Run("must be panic if unknown data type", func(t *testing.T) {
		o := New().AddObject(NewQName("test", "obj"))
		require.Panics(func() { o.AddDataField("errorField", NewQName("test", "unknown"), false) })
	})
}

func Test_SetFieldComment(t *testing.T) {
	require := require.New(t)

	obj := New().AddObject(NewQName("test", "object"))
	require.NotNil(obj)

	t.Run("must be ok to add field comment", func(t *testing.T) {
		obj.
			AddField("f1", DataKind_int64, true).
			SetFieldComment("f1", "test comment")
	})

	t.Run("must be ok to obtain field comment", func(t *testing.T) {
		require.Equal(1, obj.UserFieldCount())
		f1 := obj.Field("f1")
		require.NotNil(f1)
		require.Equal("test comment", f1.Comment())
	})

	t.Run("must be panic if unknown field name passed to comment", func(t *testing.T) {
		require.Panics(func() { obj.SetFieldComment("unknownField", "error here") })
	})
}

func Test_SetFieldVerify(t *testing.T) {
	require := require.New(t)

	obj := New().AddObject(NewQName("test", "object"))
	require.NotNil(obj)

	t.Run("must be ok to add verified field", func(t *testing.T) {
		obj.
			AddField("f1", DataKind_int64, true).
			SetFieldVerify("f1", VerificationKind_Phone).
			AddField("f2", DataKind_int64, true).
			SetFieldVerify("f2", VerificationKind_Any...)
	})

	t.Run("must be ok to obtain verified field", func(t *testing.T) {
		require.Equal(2, obj.UserFieldCount())
		f1 := obj.Field("f1")
		require.NotNil(f1)

		require.True(f1.Verifiable())
		require.False(f1.VerificationKind(VerificationKind_EMail))
		require.True(f1.VerificationKind(VerificationKind_Phone))
		require.False(f1.VerificationKind(VerificationKind_FakeLast))

		f2 := obj.Field("f2")
		require.NotNil(f2)

		require.True(f2.Verifiable())
		require.True(f2.VerificationKind(VerificationKind_EMail))
		require.True(f2.VerificationKind(VerificationKind_Phone))
		require.False(f2.VerificationKind(VerificationKind_FakeLast))
	})

	t.Run("must be panic if unknown field name passed to verify", func(t *testing.T) {
		require.Panics(func() { obj.SetFieldVerify("unknownField") })
	})
}

func Test_AddRefField(t *testing.T) {
	require := require.New(t)

	docName := NewQName("test", "doc")
	var app IAppDef

	t.Run("must be ok to add reference fields", func(t *testing.T) {
		appDef := New()
		doc := appDef.AddWDoc(docName)
		require.NotNil(doc)

		doc.
			AddField("f1", DataKind_int64, true).
			AddRefField("rf1", true).
			AddRefField("rf2", false, doc.QName())

		a, err := appDef.Build()
		require.NoError(err)

		app = a
	})

	t.Run("must be ok to work with reference fields", func(t *testing.T) {
		doc := app.WDoc(docName)

		t.Run("must be ok type cast reference field", func(t *testing.T) {
			fld := doc.Field("rf1")
			require.NotNil(fld)

			require.Equal("rf1", fld.Name())
			require.Equal(DataKind_RecordID, fld.DataKind())
			require.True(fld.Required())

			rf, ok := fld.(IRefField)
			require.True(ok)
			require.Empty(rf.Refs())
		})

		t.Run("must be ok to obtain reference field", func(t *testing.T) {
			rf2 := doc.RefField("rf2")
			require.NotNil(rf2)

			require.Equal("rf2", rf2.Name())
			require.Equal(DataKind_RecordID, rf2.DataKind())
			require.False(rf2.Required())

			require.EqualValues(QNames{docName}, rf2.Refs())
		})

		t.Run("must be nil if unknown reference field", func(t *testing.T) {
			require.Nil(doc.RefField("unknown"))
			require.Nil(doc.RefField("f1"), "must be nil because `f1` is not a reference field")
		})

		t.Run("must be ok to enumerate reference fields", func(t *testing.T) {
			require.Equal(2, func() int {
				cnt := 0
				for _, rf := range doc.RefFields() {
					cnt++
					switch cnt {
					case 1:
						require.Equal(doc.RefField("rf1"), rf)
						require.True(rf.Ref(docName))
						require.True(rf.Ref(NewQName("test", "unknown")), "must be ok because any links are allowed in the field rf1")
					case 2:
						require.EqualValues(QNames{docName}, rf.Refs())
						require.True(rf.Ref(docName))
						require.False(rf.Ref(NewQName("test", "unknown")))
					default:
						require.Failf("unexpected reference field", "field name: %s", rf.Name())
					}
				}
				return cnt
			}())
		})
	})

	t.Run("must be panic if empty field name", func(t *testing.T) {
		appDef := New()
		doc := appDef.AddWDoc(docName)
		require.Panics(func() { doc.AddRefField("", false) })
	})
}

func Test_UserFields(t *testing.T) {
	require := require.New(t)

	docName := NewQName("test", "doc")
	var app IAppDef

	t.Run("must be ok to add fields", func(t *testing.T) {
		appDef := New()
		doc := appDef.AddODoc(docName)
		require.NotNil(doc)

		doc.
			AddField("f", DataKind_int64, true).
			AddField("vf", DataKind_string, true).SetFieldVerify("vf", VerificationKind_EMail).
			AddRefField("rf", true, doc.QName())

		a, err := appDef.Build()
		require.NoError(err)

		app = a
	})

	t.Run("must be ok to enumerate user fields", func(t *testing.T) {
		doc := app.ODoc(docName)
		require.Equal(3, doc.UserFieldCount())

		require.Equal(doc.UserFieldCount(), func() int {
			cnt := 0
			for _, f := range doc.Fields() {
				if !f.IsSys() {
					cnt++
					switch cnt {
					case 1:
						require.Equal(doc.Field("f"), f)
					case 2:
						require.True(f.VerificationKind(VerificationKind_EMail))
					case 3:
						require.EqualValues(QNames{docName}, f.(IRefField).Refs())
					default:
						require.Failf("unexpected reference field", "field name: %s", f.Name())
					}
				}
			}
			return cnt
		}())
	})
}

func TestValidateRefFields(t *testing.T) {
	require := require.New(t)

	app := New()
	doc := app.AddCDoc(NewQName("test", "doc"))
	doc.AddRefField("f1", true, NewQName("test", "rec"))

	rec := app.AddCRecord(NewQName("test", "rec"))
	rec.AddRefField("f1", true, NewQName("test", "rec"))

	t.Run("must be ok if all reference field is valid", func(t *testing.T) {
		_, err := app.Build()
		require.NoError(err)
	})

	t.Run("must be error if reference field ref is not found", func(t *testing.T) {
		rec.AddRefField("f2", true, NewQName("test", "obj"))
		_, err := app.Build()
		require.ErrorIs(err, ErrNameNotFound)
		require.ErrorContains(err, "unknown type «test.obj»")
	})

	t.Run("must be error if reference field refs to non referable type", func(t *testing.T) {
		app.AddObject(NewQName("test", "obj"))
		_, err := app.Build()
		require.ErrorIs(err, ErrInvalidTypeKind)
		require.ErrorContains(err, "not a record type Object «test.obj»")
	})
}

func TestNullFields(t *testing.T) {
	require := require.New(t)

	require.Nil(NullFields.Field("field"))
	require.Zero(NullFields.FieldCount())
	require.Empty(NullFields.Fields())

	require.Nil(NullFields.RefField("field"))
	require.Empty(NullFields.RefFields())

	require.Zero(NullFields.UserFieldCount())
}
