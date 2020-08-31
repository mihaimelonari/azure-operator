package ignition

const EtcdNodeBootstrapUnit = `[Unit]
Description=Prepares the node to run ETCD.

[Service]
Type=simple
TimeoutStartSec=infinity
ExecStart=/opt/etcd-cluster-bootstrap

[Install]
WantedBy=multi-user.target
`

const EtcdUnitOverride = `[Service]
ExecStartPre=/bin/bash -c "while [ ! -f /var/lib/etcd/ssl/peer-ca.pem ]; do echo 'Waiting for /var/lib/etcd/ssl/peer-ca.pem to be written' && sleep 1; done"
ExecStartPre=/bin/bash -c "while [ ! -f /var/lib/etcd/ssl/peer-crt.pem ]; do echo 'Waiting for /var/lib/etcd/ssl/peer-crt.pem to be written' && sleep 1; done"
ExecStartPre=/bin/bash -c "while [ ! -f /var/lib/etcd/ssl/peer-key.pem ]; do echo 'Waiting for /var/lib/etcd/ssl/peer-key.pem to be written' && sleep 1; done"
EnvironmentFile=-/var/lib/etcd/cluster-environment
`

const EtcdClusterBootstrapScript = `#!/bin/bash

# This function cleans up any etcd env file leftover.
# The file will be created again by the azure operator when and if the current node has 
# to become a node in the ETCD cluster.
cleanup-old-env(){
    rm -f /etc/etcd-bootstrap-env
}

# This step waits for a virtual disk to be attached at lun0, then it ensures it is formatted and mounted in /var/lib/etcd.
prepare-disk(){
    # Wait for the disk to be attached.
    echo 'Looking for device /dev/disk/azure/scsi1/lun0'

    while [ ! -e /dev/disk/azure/scsi1/lun0 ]
    do
        echo 'Waiting for /dev/disk/azure/scsi1/lun0 to exist'
        sleep 10
    done

    echo 'Device /dev/disk/azure/scsi1/lun0 found'

    # Ensure disk is properly formatted.
    echo 'Checking if disk needs to be formatted'

    while ! /usr/sbin/blkid /dev/disk/azure/scsi1/lun0 | grep 'TYPE=\"ext4\"'>/dev/null
    do
        while ! mkfs.ext4 -L etcd /dev/disk/azure/scsi1/lun0
        do
            echo "Error formatting disk, trying again"
            sleep 1
        done
    done
    
    echo 'Disk is formatted'
    
    # Mount the disk in /var/lib/etcd.
    while [ ! -e /dev/disk/by-label/etcd ]
    do
        echo 'Waiting for /dev/disk/by-label/etcd to exist'
        sleep 1
    done

    echo "Checking if /dev/disk/by-label/etcd is mounted in /var/lib/etcd"
    while ! findmnt /var/lib/etcd >/dev/null
    do
        echo "/dev/disk/by-label/etcd isn't mounted"
        while ! /usr/bin/mount /dev/disk/by-label/etcd /var/lib/etcd -t ext4 -o rw,relatime,seclabel
        do
            echo 'Failed mounting, trying again'
            sleep 5
        done
    done
    
    echo "/dev/disk/by-label/etcd mounted successfully on /var/lib/etcd."
}

# This step waits for an ETCD cluster environment file to exist than it copies it into the etcd disk.
prepare-env(){
    # Wait for the bootstrap file to exist.
    while [ ! -f /var/lib/etcd/cluster-environment ] && [ ! -f /etc/etcd-bootstrap-env ]
    do
        echo 'Waiting for /var/lib/etcd/cluster-environment or /etc/etcd-bootstrap-env to exist'
        sleep 10
    done
    
    # Ensure the destination folder to be mounted correctly.
    while ! findmnt /var/lib/etcd >/dev/null
    do
        echo 'Waiting for /var/lib/etcd to be mounted'
        sleep 5
    done
    
    # Extract the peer certificates from the environment file.
    mkdir -p /var/lib/etcd/ssl
    . /etc/etcd-bootstrap-env
    echo "${ETCD_PEER_CA}" |base64 -d >/var/lib/etcd/ssl/peer-ca.pem
    echo "${ETCD_PEER_CRT}" |base64 -d >/var/lib/etcd/ssl/peer-crt.pem
    echo "${ETCD_PEER_KEY}" |base64 -d >/var/lib/etcd/ssl/peer-key.pem

    # Ensure not to override any existing file.
    if [ ! -f /var/lib/etcd/cluster-environment ]
    then
        cp /etc/etcd-bootstrap-env /var/lib/etcd/cluster-environment
    fi
}

join-cluster(){
    . /etc/etcd-bootstrap-env

    export ETCDCTL_API=3
    export ETCDCTL_ENDPOINTS=https://{{.Cluster.Etcd.Domain}}:{{.Cluster.Etcd.Port}}
    export ETCDCTL_CACERT=/etc/kubernetes/ssl/etcd/server-ca.pem
    export ETCDCTL_CERT=/etc/kubernetes/ssl/etcd/server-crt.pem
    export ETCDCTL_KEY=/etc/kubernetes/ssl/etcd/server-key.pem
     
    while ! etcdctl member list -w json
    do
        echo 'Waiting for ETCD cluster to be ready'
        sleep 5
    done

    # Only join additional nodes.
    if [ ${ETCD_INITIAL_CLUSTER_STATE} == 'existing' ]
    then
        echo "Joining ${ETCD_NAME} to the cluster"
		while ! etcdctl member add ${ETCD_NAME} --peer-urls=${ETCD_PEER_URL}
        do
            echo "Failed adding member, trying again"
            sleep 5
        done
    fi
    
    while ! etcdctl member list -w json | jq -r '.members[].name' | grep "${ETCD_NAME}"
    do
        echo "Waiting for node ${ETCD_NAME} to join the ETCD cluster"
        sleep 5
    done
    
    echo "Node ${ETCD_NAME} joined the ETCD cluster"
    echo "Updating ENV file in /var/lib/etcd/cluster-environment"
    
    echo -e \"ETCD_NAME=${ETCD_NAME}\nETCD_INITIAL_CLUSTER=\nETCD_INITIAL_CLUSTER_STATE=existing\n\" | tee /var/lib/etcd/cluster-environment
    
    echo 'ENV file updated'
}

cleanup-old-env
prepare-disk
prepare-env
join-cluster

`
