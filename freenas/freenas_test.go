package freenas

import (
	"testing"
)

func TestGetVolume(t *testing.T) {
	freenas := NewFreeNAS("http://192.168.67.68", "root", "freenas")
	t.Log(freenas.GetVolumeList())
}
