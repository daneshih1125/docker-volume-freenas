package freenas

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
)

type FreeNAS struct {
	username string
	password string
	url      string
	client   *http.Client
}

const VolumeURI = "/api/v1.0/storage/volume/"

type Volume struct {
	Avail      int    `json:"avail"`
	Status     string `json:"status"`
	VolGUID    string `json:"vol_guid"`
	Used       int    `json:"used"`
	Name       string `json:"name"`
	UsedPct    string `json:"used_pct"`
	ID         int    `json:"id"`
	MountPoint string `json:"mountpoint"`
}

// $ curl  -H 'Content-Type:application/json' -u root:freenas http://192.168.67.68/api/v1.0/storage/volume/freenas/ |python -m json.tool
//{
//    "avail": 59587211264,
//    "children": [
//        {
//            "avail": 57725628416,
//            "children": [
//            ...
//            ...
// The Avail feild should be the nested  aval value.
// We need to custom JSON marshalling
func (v *Volume) UnmarshalJSON(b []byte) error {
	type OrigVol Volume
	type Avail struct {
		Avail int `json:"avail"`
	}
	availInfo := &struct {
		Children []Avail `json:"children"`
		*OrigVol
	}{
		OrigVol: (*OrigVol)(v),
	}
	if err := json.Unmarshal(b, &availInfo); err != nil {
		return err
	}
	v.Avail = availInfo.Children[0].Avail
	return nil
}

type ZVolume struct {
	Name    string `json:"name"`
	VolSize int    `json:"volsize"`
}

type Service struct {
	Name   string `json:"srv_service"`
	Status bool   `json:"srv_enable"`
	ID     int    `json:"id"`
}

type ISCSITarget struct {
	Alias string `json:"iscsi_target_alias"`
	Name  string `json:"iscsi_target_name"`
	ID    int    `json:"id"`
}

type ISCSITargetGroup struct {
	PortlID  int `json:"iscsi_target_portalgroup"`
	TargetID int `json:"iscsi_target"`
	ID       int `json:"id"`
}

type ISCSIPortal struct {
	ID  int      `json:"id"`
	IPs []string `json:"iscsi_target_portal_ips"`
}

type ISCSIExtent struct {
	ID   int    `json:"id"`
	Type string `json:"iscsi_target_extent_type"`
	Name string `json:"iscsi_target_extent_name"`
	Path string `json:"iscsi_target_extent_path"`
}

type ISCSITargetToExtent struct {
	ID       int `json:"id"`
	TargetID int `json:"iscsi_target"`
	ExtentID int `json:"iscsi_extent"`
	LunID    int `json:"iscsi_lunid"`
}

func NewFreeNAS(url, username, password string) *FreeNAS {
	freenas := &FreeNAS{
		url:      url,
		username: username,
		password: password,
	}
	freenas.client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	return freenas
}

func (f *FreeNAS) HttpRequest(method string, url string, body io.Reader) (response []byte, err error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	req.SetBasicAuth(f.username, f.password)
	req.Header.Add("Content-Type", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, errors.New("HTTP Status: " + res.Status)
	}
	response, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	return response, err
}

func (f *FreeNAS) GetVolumeList() (volumes []Volume, err error) {
	response, err := f.HttpRequest("GET", f.url+VolumeURI, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &volumes); err != nil {
		log.Fatal(err)
		return nil, err
	}
	return volumes, err
}

func (f *FreeNAS) GetZFSVolumeList(volName string) (zvols []ZVolume, err error) {
	url := f.url + VolumeURI + volName + "/zvols/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &zvols); err != nil {
		log.Fatal(err)
		return nil, err
	}
	return zvols, err
}

func (f *FreeNAS) CreateZFSVolume(volName, zfsVolName string, zfsVolumeSize int) (zvol ZVolume, err error) {
	url := f.url + VolumeURI + volName + "/zvols/"
	jsonStr := fmt.Sprintf(`{"name": "%s", "volsize": "%dG"}`,
		zfsVolName, zfsVolumeSize)
	jsonData := []byte(jsonStr)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return zvol, err
	}
	if err := json.Unmarshal(response, &zvol); err != nil {
		log.Fatal(err)
		return zvol, err
	}
	return zvol, err
}

func (f *FreeNAS) DeleteZFSVolume(volName, zfsVolName string) (err error) {
	url := f.url + VolumeURI + volName + "/zvols/" + zfsVolName + "/"
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}

func (f *FreeNAS) ServicList() (services []Service, err error) {
	url := f.url + "/api/v1.0/services/services/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return services, err
	}
	if err := json.Unmarshal(response, &services); err != nil {
		return services, err
	}
	return services, err
}

func (f *FreeNAS) ServicStatus(srvName string) (service Service, err error) {
	url := f.url + "/api/v1.0/services/services/" + srvName + "/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return service, err
	}
	if err := json.Unmarshal(response, &service); err != nil {
		return service, err
	}
	return service, err
}

func (f *FreeNAS) UpdateService(srvName string, enable bool) (service Service, err error) {
	url := f.url + "/api/v1.0/services/services/" + srvName + "/"
	jsonStr := fmt.Sprintf(`{"srv_enable": %v}`, enable)
	jsonData := []byte(jsonStr)
	response, err := f.HttpRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return service, err
	}
	if err := json.Unmarshal(response, &service); err != nil {
		return service, err
	}
	return service, err
}

func (f *FreeNAS) GetISCSITargetList() (targets []ISCSITarget, err error) {
	url := f.url + "/api/v1.0/services/iscsi/target/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &targets); err != nil {
		return nil, err
	}
	return targets, err
}

func (f *FreeNAS) CreateISCSITarget(targetName string) (target ISCSITarget, err error) {
	url := f.url + "/api/v1.0/services/iscsi/target/"
	jsonStr := fmt.Sprintf(`{"iscsi_target_name": "%s"}`, targetName)
	jsonData := []byte(jsonStr)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return target, err
	}
	if err := json.Unmarshal(response, &target); err != nil {
		return target, err
	}
	return target, err
}

func (f *FreeNAS) DeleteISCSITarget(targetID int) (err error) {
	url := f.url + "/api/v1.0/services/iscsi/target/" + fmt.Sprintf("%d/", targetID)
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}

func (f *FreeNAS) GetISCSIPortalList() (portals []ISCSIPortal, err error) {
	url := f.url + "/api/v1.0/services/iscsi/portal/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &portals); err != nil {
		log.Fatal(err)
		return nil, err
	}
	return portals, err
}

func (f *FreeNAS) CreateISCSIPortal(ips []string) (portal ISCSIPortal, err error) {
	url := f.url + "/api/v1.0/services/iscsi/portal/"
	jsonMap := map[string][]string{"iscsi_target_portal_ips": ips}
	jsonData, _ := json.Marshal(jsonMap)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return portal, err
	}
	if err := json.Unmarshal(response, &portal); err != nil {
		log.Fatal(err)
		return portal, err
	}
	return portal, err
}

func (f *FreeNAS) DeleteISCSIPortal(id int) (err error) {
	url := f.url + "/api/v1.0/services/iscsi/portal/" + fmt.Sprintf("%d/", id)
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}

func (f *FreeNAS) GetISCSIExtentList() (extents []ISCSIExtent, err error) {
	url := f.url + "/api/v1.0/services/iscsi/extent/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &extents); err != nil {
		return nil, err
	}
	return extents, err
}

func (f *FreeNAS) CreateISCSIExtent(extentName, volName, zvolName string) (extent ISCSIExtent, err error) {
	url := f.url + "/api/v1.0/services/iscsi/extent/"
	jsonMap := map[string]string{
		"iscsi_target_extent_type": "Disk",
		"iscsi_target_extent_name": extentName,
		"iscsi_target_extent_disk": fmt.Sprintf("zvol/%s/%s", volName, zvolName),
	}
	jsonData, _ := json.Marshal(jsonMap)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return extent, err
	}
	if err := json.Unmarshal(response, &extent); err != nil {
		return extent, err
	}
	return extent, err
}

func (f *FreeNAS) DeleteISCSIExtent(extentID int) (err error) {
	url := f.url + "/api/v1.0/services/iscsi/extent/" + fmt.Sprintf("%d/", extentID)
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}

func (f *FreeNAS) GetISCSITargetToExtentList() (targettoextents []ISCSITargetToExtent, err error) {
	url := f.url + "/api/v1.0/services/iscsi/targettoextent/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &targettoextents); err != nil {
		return nil, err
	}
	return targettoextents, err
}

func (f *FreeNAS) CreateISCSITargetToExtent(targetID, extentID int) (targettoextent ISCSITargetToExtent, err error) {
	url := f.url + "/api/v1.0/services/iscsi/targettoextent/"
	jsonMap := map[string]int{
		"iscsi_target": targetID,
		"iscsi_extent": extentID,
	}
	jsonData, _ := json.Marshal(jsonMap)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return targettoextent, err
	}
	if err := json.Unmarshal(response, &targettoextent); err != nil {
		return targettoextent, err
	}
	return targettoextent, err
}

func (f *FreeNAS) DeleteISCSITargetToExtent(id int) (err error) {
	url := f.url + "/api/v1.0/services/iscsi/extent/" + fmt.Sprintf("%d/", id)
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}

func (f *FreeNAS) GetISCSITargetGroupList() (targetgroups []ISCSITargetGroup, err error) {
	url := f.url + "/api/v1.0/services/iscsi/targetgroup/"
	response, err := f.HttpRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &targetgroups); err != nil {
		return nil, err
	}
	return targetgroups, err
}

func (f *FreeNAS) CreateISCSITargetGroup(targetID, portalID int) (targetgroup ISCSITargetGroup, err error) {
	url := f.url + "/api/v1.0/services/iscsi/targetgroup/"
	jsonMap := map[string]interface{}{
		"iscsi_target":                targetID,
		"iscsi_target_authgroup":      nil,
		"iscsi_target_portalgroup":    portalID,
		"iscsi_target_initiatorgroup": nil,
		"iscsi_target_authtype":       "None",
		"iscsi_target_initialdigest":  "Auto",
	}
	jsonData, _ := json.Marshal(jsonMap)
	response, err := f.HttpRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return targetgroup, err
	}
	if err := json.Unmarshal(response, &targetgroup); err != nil {
		return targetgroup, err
	}
	return targetgroup, err
}

func (f *FreeNAS) DeleteISCSITargetGroup(id int) (err error) {
	url := f.url + "/api/v1.0/services/iscsi/targetgroup/" + fmt.Sprintf("%d/", id)
	_, err = f.HttpRequest("DELETE", url, nil)
	return err
}
