package sshx

import "testing"

func TestFilesystemClassification(t *testing.T) {
	raw := "Filesystem Type 1024-blocks Used Available Capacity Mounted on\n/dev/mapper/vg-root ext4 104857600 99614720 5242880 95% /\n/dev/mapper/vg-root ext4 104857600 99614720 5242880 95% /bind\nnas:/data nfs4 1048576 524288 524288 50% /network\ntmpfs tmpfs 1024 0 1024 0% /tmp\noverlay overlay 1024 0 1024 0% /container\n/dev/sdb btrfs 1048576 524288 524288 50% /pool\n"
	disks := parseDisk(raw)
	if len(disks) != 4 {
		t.Fatalf("expected all local mount points, network, pool: %+v", disks)
	}
	if disks[0].UsagePct != 95 || disks[0].TotalBytes != 107374182400 {
		t.Fatalf("lost root warning or precision: %+v", disks[0])
	}
	if !disks[2].Network {
		t.Fatal("network classified local")
	}
	snap, _, _, _ := parseSysInfoLoose("---DISK---\n" + raw)
	if !snap.DiskIncomplete || !snap.DiskUsage[3].Uncertain {
		t.Fatal("pool must not be blindly summed")
	}
}
