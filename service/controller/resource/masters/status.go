package masters

const (
	// Types
	Stage                        = "Stage"
	DeploymentTemplateChecksum   = "TemplateChecksum"
	DeploymentParametersChecksum = "ParametersChecksum"

	// States
	BackupETCDDisk                 = "BackupETCDDisk"
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
