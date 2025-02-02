/*
* Copyright (c) 2021-present unTill Pro, Ltd.
* @author Maxim Geraskin
*
 */

package istructs

import (
	"context"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
)

//go:generate stringer -type=ResourceKindType
type ResourceKindType uint8

const (
	ResourceKind_null ResourceKindType = iota
	ResourceKind_CommandFunction
	ResourceKind_QueryFunction
	ResourceKind_FakeLast
)

type IResource interface {
	// Ref. ResourceKind_* constants
	Kind() ResourceKindType
	QName() appdef.QName
}

// ******************* Functions **************************

type IFunction interface {
	IResource
}

type ICommandFunction interface {
	IFunction
	Exec(args ExecCommandArgs) error
}

type IQueryFunction interface {
	IFunction
	// panics if created by not NewQueryFunctionCustomResult(). Actually needed for q.sys.Collection only
	ResultType(args PrepareArgs) appdef.QName
	Exec(ctx context.Context, args ExecQueryArgs, callback ExecQueryCallback) error
}

type PrepareArgs struct {
	Workpiece      interface{}
	ArgumentObject IObject
	Workspace      WSID
}

type CommandPrepareArgs struct {
	PrepareArgs
	ArgumentUnloggedObject IObject
}

type ExecCommandArgs struct {
	CommandPrepareArgs
	State   IState
	Intents IIntents
}

type ExecQueryCallback func(object IObject) error

type ExecQueryArgs struct {
	PrepareArgs
	State IState
}

type IState interface {
	// NewKey returns a Key builder for specified storage and entity name
	KeyBuilder(storage, entity appdef.QName) (builder IStateKeyBuilder, err error)

	CanExist(key IStateKeyBuilder) (value IStateValue, ok bool, err error)

	CanExistAll(keys []IStateKeyBuilder, callback StateValueCallback) (err error)

	MustExist(key IStateKeyBuilder) (value IStateValue, err error)

	MustExistAll(keys []IStateKeyBuilder, callback StateValueCallback) (err error)

	MustNotExist(key IStateKeyBuilder) (err error)

	MustNotExistAll(keys []IStateKeyBuilder) (err error)

	// Read reads all values according to the get and return them in callback
	Read(key IStateKeyBuilder, callback ValueCallback) (err error)
}
type IIntents interface {
	// NewValue returns a new value builder for given get
	// If a value with the same get already exists in storage, it will be replaced
	NewValue(key IStateKeyBuilder) (builder IStateValueBuilder, err error)

	// UpdateValue returns a value builder to update existing value
	UpdateValue(key IStateKeyBuilder, existingValue IStateValue) (builder IStateValueBuilder, err error)
}
type IStateValue interface {
	IValue
	AsValue(name string) IStateValue
	Length() int
	GetAsString(index int) string
	GetAsBytes(index int) []byte
	GetAsInt32(index int) int32
	GetAsInt64(index int) int64
	GetAsFloat32(index int) float32
	GetAsFloat64(index int) float64
	GetAsQName(index int) appdef.QName
	GetAsBool(index int) bool
	GetAsValue(index int) IStateValue
}
type IStateValueBuilder interface {
	IValueBuilder
	BuildValue() IStateValue
}
type IStateKeyBuilder interface {
	IKeyBuilder
	Storage() appdef.QName
	Entity() appdef.QName
}
type StateValueCallback func(key IKeyBuilder, value IStateValue, ok bool) (err error)
type ValueCallback func(key IKey, value IStateValue) (err error)

//go:generate stringer -type=RateLimitKind
type RateLimitKind uint8

type RateLimit struct {
	Period                time.Duration
	MaxAllowedPerDuration uint32
}
