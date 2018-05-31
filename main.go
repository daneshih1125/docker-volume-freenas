package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/daneshih1125/docker-volume-freenas/freenas"
	"github.com/daneshih1125/docker-volume-freenas/utils"
	"github.com/docker/go-plugins-helpers/volume"
)

const socketAddress = "/run/docker/plugins/freenas.sock"
const iscsiService = "iscsitarget"

type FreeNASISCSIVolume struct {
	Size             int
	Name             string
	Mountpoint       string
	TargetID         int
	ExtentID         int
	TargetGroupID    int
	TargetToExtentID int
	PoolName         string
	connections      int
}

type FreeNASISCSIDriver struct {
	sync.RWMutex

	root          string
	statePath     string
	url           string
	hostname      string
	username      string
	password      string
	volumes       map[string]*FreeNASISCSIVolume
	freenas       *freenas.FreeNAS
	freenasPortal int
}

func newFreeNASISCSIDriver(root, furl, username, password string) (*FreeNASISCSIDriver, error) {
	log.WithField("method", "new driver").Debug(root)

	fi, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, errors.New(fmt.Sprintf("%s is not exist", root))
	} else if err != nil {
		return nil, err
	}
	if fi != nil && !fi.IsDir() {
		return nil, errors.New(fmt.Sprintf("%s already exist and it's not a directory", root))
	}

	d := &FreeNASISCSIDriver{
		root:      filepath.Join(root, "volumes"),
		url:       furl,
		username:  username,
		password:  password,
		statePath: filepath.Join(root, "freenas-state.json"),
		volumes:   map[string]*FreeNASISCSIVolume{},
	}
	u, err := url.Parse(d.url)
	if err != nil {
		return nil, err
	}
	d.hostname = u.Hostname()
	d.freenas = freenas.NewFreeNAS(d.url, d.username, d.password)
	iscsiSrv, err := d.freenas.ServicStatus(iscsiService)
	if iscsiSrv.Status == false {
		_, err = d.freenas.UpdateService(iscsiService, true)
		if err != nil {
			return nil, err
		}
	}
	portals, err := d.freenas.GetISCSIPortalList()
	if err != nil {
		return nil, err
	}
	allowAny := false
	for _, p := range portals {
		for _, ip := range p.IPs {
			if ip == "0.0.0.0:3260" {
				allowAny = true
				break
			}
		}
		if allowAny == true {
			d.freenasPortal = p.ID
			break
		}
	}
	if allowAny == false {
		p, err := d.freenas.CreateISCSIPortal([]string{"0.0.0.0:3260"})
		if err != nil {
			return nil, err
		}
		d.freenasPortal = p.ID
	}
	log.WithField("freenasPortal", d.freenasPortal).Info("0.0.0.0:3260 portal ID")

	data, err := ioutil.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *FreeNASISCSIDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		log.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		log.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *FreeNASISCSIDriver) Create(r *volume.CreateRequest) error {
	log.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &FreeNASISCSIVolume{}

	for key, val := range r.Options {
		if key == "size" {
			v.Size, _ = strconv.Atoi(val)
		}
	}
	if v.Size == 0 {
		return errors.New("Invalid size value")
	}
	// find the volume that has maximum available size
	volume := freenas.Volume{}
	freeVols, err := d.freenas.GetVolumeList()
	for _, vol := range freeVols {
		if vol.Avail > volume.Avail {
			volume = vol
		}
	}
	if volume.Avail < 1024*1024*1024*v.Size {
		return errors.New("Insufficient volume size")
	}
	// FreeNAS iscsi volume name
	v.Name = "docker-" + r.Name
	v.PoolName = volume.Name
	// Create ZVOL
	_, err = d.freenas.CreateZFSVolume(volume.Name, v.Name, v.Size)
	if err != nil {
		return err
	}
	// Create iSCSI target
	target, err := d.freenas.CreateISCSITarget(v.Name)
	if err != nil {
		return err
	}
	v.TargetID = target.ID
	// Create iSCSI target group
	tgroup, err := d.freenas.CreateISCSITargetGroup(target.ID, d.freenasPortal)
	if err != nil {
		return err
	}
	v.TargetGroupID = tgroup.ID
	// Create iSCSI extent
	extent, err := d.freenas.CreateISCSIExtent(v.Name, volume.Name, v.Name)
	if err != nil {
		return err
	}
	v.ExtentID = extent.ID
	// Create iSCSI target to extent
	targettoextent, err := d.freenas.CreateISCSITargetToExtent(target.ID, extent.ID)
	if err != nil {
		return err
	}
	v.TargetToExtentID = targettoextent.ID
	v.Mountpoint = filepath.Join(d.root, r.Name)
	if err != nil {
		return err
	}
	d.volumes[r.Name] = v
	d.saveState()
	return nil
}

func (d *FreeNASISCSIDriver) List() (*volume.ListResponse, error) {
	log.WithField("method", "list").Debugf("")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *FreeNASISCSIDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.GetResponse{}, errors.New("volume not found")
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}, nil
}

func (d *FreeNASISCSIDriver) Remove(r *volume.RemoveRequest) error {
	log.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return errors.New("Volume not found")
	}

	if v.connections != 0 {
		return errors.New(fmt.Sprintf("volume %s is currently used by a container", r.Name))
	}
	d.freenas.DeleteISCSITargetToExtent(v.TargetToExtentID)
	d.freenas.DeleteISCSIExtent(v.ExtentID)
	d.freenas.DeleteISCSITargetGroup(v.TargetGroupID)
	d.freenas.DeleteISCSITarget(v.TargetID)
	d.freenas.DeleteZFSVolume(v.PoolName, v.Name)
	delete(d.volumes, r.Name)
	d.saveState()
	return nil
}

func (d *FreeNASISCSIDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	log.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.PathResponse{}, errors.New("volume not found")
	}

	return &volume.PathResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *FreeNASISCSIDriver) mountVolume(v *FreeNASISCSIVolume) error {
	iqn, err := utils.FindISCSIIQN(d.hostname, v.Name)
	if err != nil {
		return err
	}
	utils.LoginISCSITarget(iqn)
	diskpath, err := utils.GetISCSIDiskPath(d.hostname, v.Name)
	if err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(diskpath); os.IsNotExist(err) {
			time.Sleep(time.Second)
			continue
		} else {
			break
		}
	}
	if utils.GetBlkDevType(diskpath) != "xfs" {
		err = utils.FormatXFS(diskpath)
	}
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("mount %s %s", diskpath, v.Mountpoint)
	return exec.Command("sh", "-c", cmd).Run()
}

func (d *FreeNASISCSIDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		log.Fatal("Volume not fount")
		return &volume.MountResponse{}, errors.New("Volume not found")
	}
	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				log.Fatal("Failed to mkdir")
				return &volume.MountResponse{}, err
			}
		} else if err != nil {
			log.Fatal("other error")
			return &volume.MountResponse{}, err
		}

		if fi != nil && !fi.IsDir() {
			log.Fatal("not dir")
			return &volume.MountResponse{}, errors.New("already exist and it's not a directory")
		}

		if err := d.mountVolume(v); err != nil {
			return &volume.MountResponse{}, err
		}
	}
	v.connections++
	return &volume.MountResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *FreeNASISCSIDriver) unmountVolume(v *FreeNASISCSIVolume) error {
	iqn, err := utils.FindISCSIIQN(d.hostname, v.Name)
	cmd := fmt.Sprintf("umount %s", v.Mountpoint)
	err = exec.Command("sh", "-c", cmd).Run()
	if err != nil {
		return err
	}
	return utils.LogoutISCSITarget(iqn)
}

func (d *FreeNASISCSIDriver) Unmount(r *volume.UnmountRequest) error {
	log.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return errors.New("volume not found")
	}
	v.connections--
	if v.connections <= 0 {
		if err := d.unmountVolume(v); err != nil {
			return err
		}
		v.connections = 0
	}
	return nil
}

func (d *FreeNASISCSIDriver) Capabilities() *volume.CapabilitiesResponse {
	log.WithField("method", "capabilities").Debugf("")

	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "local"},
	}
}

func main() {
	apiURL := os.Getenv("FREENAS_API_URL")
	apiUsername := os.Getenv("FREENAS_API_USER")
	apiPassword := os.Getenv("FREENAS_API_PASSWORD")
	if apiURL == "" || apiUsername == "" || apiPassword == "" {
		log.Fatal("Invalid environment variables: FREENAS_API_URL, FREENAS_API_USER, FREENAS_API_PASSWORD")
	}
	d, err := newFreeNASISCSIDriver("/mnt/freenas", apiURL, apiUsername, apiPassword)
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	log.SetLevel(log.DebugLevel)
	log.Infof("listening on %s", socketAddress)
	log.Error(h.ServeUnix(socketAddress, 0))
}
