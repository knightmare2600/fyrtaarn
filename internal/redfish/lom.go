package redfish

// Oracle ILOM virtual media is not built. ILOM mounts an NFS or Samba image
// through its CLI over SSH (set /SP/services/kvms/host_storage_device/remote/
// server_URI=nfs://..., then set .../host_storage_device mode=remote; ILOM
// 4.0.x and 5.x administration guides), and fyrtaarn has no SSH client: it
// would need golang.org/x/crypto/ssh, which Robert chose not to add for
// 0.9.2 (2026-11-24). Oracle's guides read do not describe Redfish virtual
// media for ILOM.
