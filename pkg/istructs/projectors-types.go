/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package istructs

import "github.com/voedger/voedger/pkg/appdef"

type Projector struct {
	Name appdef.QName
	Func func(event IPLogEvent, state IState, intents IIntents) (err error)
}

// ProjectorFactory creates a istructs.Projector
type ProjectorFactory func(partition PartitionID) Projector
