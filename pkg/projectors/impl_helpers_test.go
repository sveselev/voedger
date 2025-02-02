/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 *
 * @author Michael Saigachenko
 */

package projectors

import (
	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
)

const (
	colValue = "myvalue"
)

type plogEvent struct {
	wlogOffset istructs.Offset
	wsid       istructs.WSID
}

var testQName = appdef.NewQName(appdef.SysPackage, "abc")

func (e *plogEvent) ArgumentObject() istructs.IObject     { return istructs.NewNullObject() }
func (e *plogEvent) Command() istructs.IObject            { return nil }
func (e *plogEvent) Workspace() istructs.WSID             { return e.wsid }
func (e *plogEvent) WLogOffset() istructs.Offset          { return e.wlogOffset }
func (e *plogEvent) SaveWLog() (err error)                { return nil }
func (e *plogEvent) SaveCUDs() (err error)                { return nil }
func (e *plogEvent) Release()                             {}
func (e *plogEvent) Error() istructs.IEventError          { return nil }
func (e *plogEvent) QName() appdef.QName                  { return testQName }
func (e *plogEvent) CUDs(func(rec istructs.ICUDRow))      {}
func (e *plogEvent) RegisteredAt() istructs.UnixMilli     { return 0 }
func (e *plogEvent) Synced() bool                         { return false }
func (e *plogEvent) DeviceID() istructs.ConnectedDeviceID { return 0 }
func (e *plogEvent) SyncedAt() istructs.UnixMilli         { return 0 }

func storeProjectorOffset(appStructs istructs.IAppStructs, partition istructs.PartitionID, projectorName appdef.QName, offset istructs.Offset) error {
	kb := appStructs.ViewRecords().KeyBuilder(qnameProjectionOffsets)
	kb.PutInt32(partitionFld, int32(partition))
	kb.PutQName(projectorNameFld, projectorName)
	vb := appStructs.ViewRecords().NewValueBuilder(qnameProjectionOffsets)
	vb.PutInt64(offsetFld, int64(offset))
	return appStructs.ViewRecords().Put(istructs.NullWSID, kb, vb)
}

func getActualizerOffset(require *require.Assertions, appStructs istructs.IAppStructs, partition istructs.PartitionID, projectorName appdef.QName) istructs.Offset {
	offs, err := ActualizerOffset(appStructs, partition, projectorName)
	require.NoError(err)
	return offs
}

func getProjectionValue(require *require.Assertions, appStructs istructs.IAppStructs, qname appdef.QName, wsid istructs.WSID) int32 {
	key := appStructs.ViewRecords().KeyBuilder(qname)
	key.PutInt32("pk", 0)
	key.PutInt32("cc", 0)
	value, err := appStructs.ViewRecords().Get(wsid, key)
	if err == istructsmem.ErrRecordNotFound {
		return 0
	}
	require.NoError(err)
	return value.AsInt32("myvalue")
}
