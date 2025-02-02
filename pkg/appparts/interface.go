/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appparts

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/istructs"
)

// Application partitions manager.
type IAppPartitions interface {
	// Adds new application or update existing.
	//
	// If application with the same name exists, then its definition will be updated.
	//
	// @ConcurrentAccess
	DeployApp(name istructs.AppQName, def appdef.IAppDef, partsCount int, engines [cluster.ProcessorKind_Count]int)

	// Deploys new partitions for specified application or update existing.
	//
	// If partition with the same app and id already exists, it will be updated.
	//
	// # Panics:
	// 	- if application not exists
	//
	// @ConcurrentAccess
	DeployAppPartitions(appName istructs.AppQName, partIDs []istructs.PartitionID)

	// Returns application definition.
	//
	// Returns nil and error if app not exists.
	AppDef(istructs.AppQName) (appdef.IAppDef, error)

	// Returns application partitions count.
	//
	// Returns 0 and error if app not exists.
	AppPartsCount(istructs.AppQName) (int, error)

	// Borrows and returns a partition.
	//
	// If partition not exist, returns error.
	//
	// @ConcurrentAccess
	Borrow(istructs.AppQName, istructs.PartitionID, cluster.ProcessorKind) (IAppPartition, error)
}

// Application partition.
type IAppPartition interface {
	App() istructs.AppQName
	ID() istructs.PartitionID

	AppStructs() istructs.IAppStructs

	// Releases borrowed partition.
	//
	// @ConcurrentAccess
	Release()
}
