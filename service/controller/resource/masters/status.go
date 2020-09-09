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
	CreateSnapshot                 = "CreateSnapshot"
	ClusterUpgradeRequirementCheck = "ClusterUpgradeRequirementCheck"
	DeploymentUninitialized        = "DeploymentUninitialized"
	DeploymentInitialized          = "DeploymentInitialized"
	DeploymentCompleted            = "DeploymentCompleted"
	Empty                          = ""
	ETCDMigrationCheck             = "ETCDMigrationCheck"
	MasterInstancesUpgrading       = "MasterInstancesUpgrading"
	ProvisioningSuccessful         = "ProvisioningSuccessful"
	WaitForMastersToBecomeReady    = "WaitForMastersToBecomeReady"
)
