package utils

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

func FindISCSIIQN(hostname, targetname string) (iqn string, err error) {
	out, err := exec.Command("iscsiadm", "-m", "discovery", "-t", "st", "-p", hostname).Output()
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasSuffix(line, ":"+targetname) {
			iqn = strings.Split(line, " ")[1]
		}
	}
	if iqn == "" {
		return "", errors.New("IQN not found")
	}
	return iqn, nil
}

func LoginISCSITarget(iqn string) error {
	cmd := fmt.Sprintf("iscsiadm -m node --targetname=%s --login", iqn)
	return exec.Command("sh", "-c", cmd).Run()
}

func LogoutISCSITarget(iqn string) error {
	cmd := fmt.Sprintf("iscsiadm -m node --targetname=%s --logout", iqn)
	return exec.Command("sh", "-c", cmd).Run()
}

func GetISCSIDiskPath(hostname, targetname string) (diskpath string, err error) {
	out, err := exec.Command("iscsiadm", "-m", "discovery", "-t", "st", "-p", hostname).Output()
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var discovery string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasSuffix(line, ":"+targetname) {
			discovery = line
		}
	}
	if discovery == "" {
		return "", errors.New("Target not found")
	}
	address, iqn := strings.Split(discovery, " ")[0], strings.Split(discovery, " ")[1]
	diskpath = fmt.Sprintf("/dev/disk/by-path/ip-%s-iscsi-%s-lun-0", strings.Split(address, ",")[0], iqn)
	return diskpath, err
}

func GetBlkDevType(devpath string) (blktype string) {
	out, _ := exec.Command("blkid", devpath).Output()
	re := regexp.MustCompile(`TYPE="([^"]*)"`)
	m := re.FindStringSubmatch(string(out))
	if len(m) == 0 {
		return ""
	}
	return m[1]
}

func FormatXFS(diskpath string) error {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("mkfs.xfs %s", diskpath))
	_, err := cmd.CombinedOutput()
	return err
}
