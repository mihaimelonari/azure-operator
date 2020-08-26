package ignition

const EtcdFormatUnit = `[Unit]
Description=Waits for ETCD disk to be attached and then formats and mounts it.

[Service]
Type=simple
TimeoutStartSec=infinity
ExecStartPre=/bin/bash -c "while [ ! -e /dev/disk/azure/scsi1/lun0 ]; do echo 'Waiting for /dev/disk/azure/scsi1/lun0 to exist' && sleep 1; done"
ExecStartPre=/bin/bash -c "((blkid /dev/disk/azure/scsi1/lun0 | grep 'TYPE=\"ext4\"'>/dev/null) && echo 'Disk is already formatted') || mkfs.ext4 -L etcd /dev/disk/azure/scsi1/lun0"
ExecStartPre=/bin/bash -c "while [ ! -e /dev/disk/by-label/etcd ]; do echo 'Waiting for /dev/disk/by-label/etcd to exist' && sleep 1; done"
ExecStart=/bin/bash -c "findmnt /var/lib/etcd || /usr/bin/mount /dev/disk/by-label/etcd /var/lib/etcd -t ext4 -o rw,relatime,seclabel"

[Install]
WantedBy=multi-user.target
`

const EtcdUnitOverride = `[Unit]
After=etcd-disk-format.service
Requires=etcd-disk-format.service
`
