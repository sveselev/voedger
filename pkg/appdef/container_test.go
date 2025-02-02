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

func Test_type_AddContainer(t *testing.T) {
	require := require.New(t)

	appDef := New()
	root := appDef.AddObject(NewQName("test", "root"))
	require.NotNil(root)

	childName := NewQName("test", "child")
	_ = appDef.AddObject(childName)

	t.Run("must be ok to add container", func(t *testing.T) {
		root.AddContainer("c1", childName, 1, Occurs_Unbounded)

		require.Equal(1, root.ContainerCount())
		c := root.Container("c1")
		require.NotNil(c)

		require.Equal("c1", c.Name())

		require.Equal(childName, c.QName())
		typ := c.Type()
		require.NotNil(typ)
		require.Equal(childName, typ.QName())
		require.Equal(TypeKind_Object, typ.Kind())

		require.EqualValues(1, c.MinOccurs())
		require.Equal(Occurs_Unbounded, c.MaxOccurs())
	})

	t.Run("chain notation is ok to add containers", func(t *testing.T) {
		obj := New().AddObject(NewQName("test", "obj"))
		n := obj.AddContainer("c1", childName, 1, Occurs_Unbounded).
			AddContainer("c2", childName, 1, Occurs_Unbounded).
			AddContainer("c3", childName, 1, Occurs_Unbounded).(IType).QName()
		require.Equal(obj.QName(), n)
		require.Equal(3, obj.ContainerCount())
	})

	t.Run("must be panic if empty container name", func(t *testing.T) {
		require.Panics(func() { root.AddContainer("", childName, 1, Occurs_Unbounded) })
	})

	t.Run("must be panic if invalid container name", func(t *testing.T) {
		require.Panics(func() { root.AddContainer("naked_🔫", childName, 1, Occurs_Unbounded) })
	})

	t.Run("must be panic if container name dupe", func(t *testing.T) {
		require.Panics(func() { root.AddContainer("c1", childName, 1, Occurs_Unbounded) })
	})

	t.Run("must be panic if container type name missed", func(t *testing.T) {
		require.Panics(func() { root.AddContainer("c2", NullQName, 1, Occurs_Unbounded) })
	})

	t.Run("must be panic if invalid occurrences", func(t *testing.T) {
		require.Panics(func() { root.AddContainer("c2", childName, 1, 0) })
		require.Panics(func() { root.AddContainer("c3", childName, 2, 1) })
	})

	t.Run("must be panic if container type is incompatible", func(t *testing.T) {
		docName := NewQName("test", "doc")
		_ = appDef.AddCDoc(docName)
		require.Panics(func() { root.AddContainer("c2", docName, 1, 1) })
		require.Nil(root.Container("c2"))
	})

	t.Run("must be panic if too many containers", func(t *testing.T) {
		el := New().AddObject(childName)
		for i := 0; i < MaxTypeContainerCount; i++ {
			el.AddContainer(fmt.Sprintf("c_%#x", i), childName, 0, Occurs_Unbounded)
		}
		require.Panics(func() { el.AddContainer("errorContainer", childName, 0, Occurs_Unbounded) })
	})
}

func TestValidateContainer(t *testing.T) {
	require := require.New(t)

	app := New()
	doc := app.AddCDoc(NewQName("test", "doc"))
	doc.AddContainer("rec", NewQName("test", "rec"), 0, Occurs_Unbounded)

	t.Run("must be error if container type not found", func(t *testing.T) {
		_, err := app.Build()
		require.ErrorIs(err, ErrNameNotFound)
		require.ErrorContains(err, "unknown type «test.rec»")
	})

	rec := app.AddCRecord(NewQName("test", "rec"))
	_, err := app.Build()
	require.NoError(err)

	t.Run("must be ok container recurse", func(t *testing.T) {
		rec.AddContainer("rec", NewQName("test", "rec"), 0, Occurs_Unbounded)
		_, err := app.Build()
		require.NoError(err)
	})

	t.Run("must be ok container sub recurse", func(t *testing.T) {
		rec.AddContainer("rec1", NewQName("test", "rec1"), 0, Occurs_Unbounded)
		rec1 := app.AddCRecord(NewQName("test", "rec1"))
		rec1.AddContainer("rec", NewQName("test", "rec"), 0, Occurs_Unbounded)
		_, err := app.Build()
		require.NoError(err)
	})

	t.Run("must be error if container kind is incompatible", func(t *testing.T) {
		doc.AddContainer("obj", NewQName("test", "obj"), 0, 1)
		_ = app.AddObject(NewQName("test", "obj"))
		_, err := app.Build()
		require.ErrorIs(err, ErrInvalidTypeKind)
		require.ErrorContains(err, "«CDoc» can`t contain «Object»")
	})
}
