/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mount

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/golang/glog"
)

// FakeMounter implements mount.Interface for tests.
type FakeMounter struct {
	MountPoints []MountPoint
	Log         []FakeAction
	// Some tests run things in parallel, make sure the mounter does not produce
	// any golang's DATA RACE warnings.
	mutex sync.Mutex
}

var _ Interface = &FakeMounter{}

// Values for FakeAction.Action
const FakeActionMount = "mount"
const FakeActionUnmount = "unmount"

// FakeAction objects are logged every time a fake mount or unmount is called.
type FakeAction struct {
	Action string // "mount" or "unmount"
	Target string // applies to both mount and unmount actions
	Source string // applies only to "mount" actions
	FSType string // applies only to "mount" actions
}

func (f *FakeMounter) ResetLog() {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.Log = []FakeAction{}
}

func (f *FakeMounter) Mount(source string, target string, fstype string, options []string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

        absSource, err := filepath.EvalSymlinks(source)
        if err != nil {
		absSource = source
        }

	// find 'bind' option
	for _, option := range options {
		if option == "bind" {
			bindFound := false
			// This is a bind-mount. In order to mimic linux behaviour, we must
			// use the original device of the bind-mount as the real source.
			// E.g. when mounted /dev/sda like this:
			//      $ mount /dev/sda /mnt/test
			//      $ mount -o bind /mnt/test /mnt/bound
			// then /proc/mount contains:
			// /dev/sda /mnt/test
			// /dev/sda /mnt/bound
			// (and not /mnt/test /mnt/bound)
			// I.e. we must use /dev/sda as source instead of /mnt/test in the
			// bind mount.

			for _, mnt := range f.MountPoints {
				if absSource == mnt.Path {
					absSource = mnt.Device
					bindFound = true
					break
				}
			}
			if bindFound == false {
				return fmt.Errorf("failed to find bind mount for source: %v", source)
			}
		}
	}

	// If target is a symlink, get its absolute path
	absTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		absTarget = target
	}
	fmt.Printf("IDC=== target: %v, absTarget: %v\n", target, absTarget)
	
	f.MountPoints = append(f.MountPoints, MountPoint{Device: absSource, Path: absTarget, Type: fstype})
	glog.V(5).Infof("Fake mounter: mounted %s to %s", absSource, absTarget)
	f.Log = append(f.Log, FakeAction{Action: FakeActionMount, Target: absTarget, Source: absSource, FSType: fstype})
	return nil
}

func (f *FakeMounter) Unmount(target string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// If target is a symlink, get its absolute path
	absTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		absTarget = target
	}

	newMountpoints := []MountPoint{}
	for _, mp := range f.MountPoints {
		if mp.Path == absTarget {
			glog.V(5).Infof("Fake mounter: unmounted %s from %s", mp.Device, absTarget)
			// Don't copy it to newMountpoints
			continue
		}
		newMountpoints = append(newMountpoints, MountPoint{Device: mp.Device, Path: mp.Path, Type: mp.Type})
	}
	f.MountPoints = newMountpoints
	f.Log = append(f.Log, FakeAction{Action: FakeActionUnmount, Target: absTarget})
	return nil
}

func (f *FakeMounter) List() ([]MountPoint, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.MountPoints, nil
}

func (f *FakeMounter) IsMountPointMatch(mp MountPoint, dir string) bool {
	return mp.Path == dir
}

func (f *FakeMounter) IsNotMountPoint(dir string) (bool, error) {
	return IsNotMountPoint(f, dir)
}

func (f *FakeMounter) IsLikelyNotMountPoint(file string) (bool, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	_, err := os.Stat(file)
	if err != nil {
		return true, err
	}

	// If file is a symlink, get its absolute path
	absFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		absFile = file
	}

	for _, mp := range f.MountPoints {
		if mp.Path == absFile {
			glog.V(5).Infof("isLikelyNotMountPoint for %s: mounted %s, false", file, mp.Path)
			return false, nil
		}
	}
	glog.V(5).Infof("isLikelyNotMountPoint for %s: true", file)
	return true, nil
}

func (f *FakeMounter) DeviceOpened(pathname string) (bool, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	for _, mp := range f.MountPoints {
		if mp.Device == pathname {
			return true, nil
		}
	}
	return false, nil
}

func (f *FakeMounter) PathIsDevice(pathname string) (bool, error) {
	return true, nil
}

func (f *FakeMounter) GetDeviceNameFromMount(mountPath, pluginDir string) (string, error) {
	// IDC FIX for darwin, since falling through to unsupported which is empty
	fmt.Printf("IDC GetDeviceNameFromMount mountPath: %v, pluginDir %v\n",mountPath,pluginDir)
	if runtime.GOOS == "darwin" {
		path.Base(mountPath)
		//dev,_,err := GetDeviceNameFromMount(f, mountPath)
		//return dev, err
		return GetDeviceNameFromMountDarwin(f, mountPath, pluginDir)
	} else {
		return getDeviceNameFromMount(f, mountPath, pluginDir)
	}
}

// getDeviceNameFromMount find the device name from /proc/mounts in which
// the mount path reference should match the given plugin directory. In case no mount path reference
// matches, returns the volume name taken from its given mountPath
func GetDeviceNameFromMountDarwin(mounter Interface, mountPath, pluginDir string) (string, error) {
	refs, err := GetMountRefsByDev(mounter, mountPath)
	if err != nil {
		glog.V(4).Infof("GetMountRefs failed for mount path %q: %v", mountPath, err)
		return "", err
	}
	if len(refs) == 0 {
		glog.V(4).Infof("Directory %s is not mounted", mountPath)
		return "", fmt.Errorf("directory %s is not mounted", mountPath)
	}
	basemountPath := path.Join(pluginDir, MountsInGlobalPDPath)
	fmt.Printf("IDC1=========basemountPath:%v\n",basemountPath)
	for _, ref := range refs {
		if strings.HasPrefix(ref, basemountPath) {
			fmt.Printf("IDC1=========ref:%v\n",ref)
			volumeID, err := filepath.Rel(basemountPath, ref)
			if err != nil {
				glog.Errorf("Failed to get volume id from mount %s - %v", mountPath, err)
				return "", err
			}
			fmt.Printf("IDC1=========volumeID:%v\n",volumeID)
			return volumeID, nil
		}
	}
	fmt.Println("IDC2=========mountPath:%v",mountPath)
	return path.Base(mountPath), nil
}

func (f *FakeMounter) MakeRShared(path string) error {
	return nil
}

func (f *FakeMounter) GetFileType(pathname string) (FileType, error) {
	return FileType("fake"), nil
}

func (f *FakeMounter) MakeDir(pathname string) error {
	return nil
}

func (f *FakeMounter) MakeFile(pathname string) error {
	return nil
}

func (f *FakeMounter) ExistsPath(pathname string) bool {
	return false
}
