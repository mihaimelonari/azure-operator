package masters

const (
	// Types
	Stage                        = "Stage"
	DeploymentTemplateChecksum   = "TemplateChecksum"
	DeploymentParametersChecksum = "ParametersChecksum"

	// States
	BackupETCDDisk                 = "BackupETCDDisk"
	WaitForMastersToDrain          = "WaitForMastersToDrain"
	StopMasters                    = "StopMasters"
	EnsureSnapshot                 = "EnsureSnapshot"
	ClusterUpgradeRequirementCheck = "ClusterUpgradeRequirementCheck"
	DeploymentUninitialized        = "DeploymentUninitialized"
	DeploymentInitialized          = "DeploymentInitialized"
	DeploymentCompleted            = "DeploymentCompleted"
	Empty                          = ""
	UpdateMaster                   = "UpdateMaster"
	ReimageMaster                  = "ReimageMaster"
	StartMaster                    = "StartMaster"
	MasterInstancesUpgrading       = "MasterInstancesUpgrading"
	ProvisioningSuccessful         = "ProvisioningSuccessful"
	WaitForMastersToBecomeReady    = "WaitForMastersToBecomeReady"
)
