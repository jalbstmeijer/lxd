package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/lxc/lxd"
	"gopkg.in/lxc/go-lxc.v2"
)

func containersPost(d *Daemon, r *http.Request) Response {
	lxd.Debugf("responding to create")

	if d.id_map == nil {
		return BadRequest(fmt.Errorf("lxd's user has no subuids"))
	}

	raw := lxd.Jmap{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return BadRequest(err)
	}

	name, err := raw.GetString("name")
	if err != nil {
		/* TODO: namegen code here */
		name = "foo"
	}

	source, err := raw.GetMap("source")
	if err != nil {
		return BadRequest(err)
	}

	type_, err := source.GetString("type")
	if err != nil {
		return BadRequest(err)
	}

	url, err := source.GetString("url")
	if err != nil {
		return BadRequest(err)
	}

	imageName, err := source.GetString("name")
	if err != nil {
		return BadRequest(err)
	}

	/* TODO: support other options here */
	if type_ != "remote" {
		return NotImplemented
	}

	if url != "https+lxc-images://images.linuxcontainers.org" {
		return NotImplemented
	}

	if imageName != "lxc-images/ubuntu/trusty/amd64" {
		return NotImplemented
	}

	opts := lxc.TemplateOptions{
		Template: "download",
		Distro:   "ubuntu",
		Release:  "trusty",
		Arch:     "amd64",
	}

	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return InternalError(err)
	}

	/*
	 * Set the id mapping. This may not be how we want to do it, but it's a
	 * start.  First, we remove any id_map lines in the config which might
	 * have come from ~/.config/lxc/default.conf.  Then add id mapping based
	 * on Domain.id_map
	 */
	if d.id_map != nil {
		lxd.Debugf("setting custom idmap")
		err = c.SetConfigItem("lxc.id_map", "")
		if err != nil {
			lxd.Debugf("Failed to clear id mapping, continuing")
		}
		uidstr := fmt.Sprintf("u 0 %d %d\n", d.id_map.Uidmin, d.id_map.Uidrange)
		lxd.Debugf("uidstr is %s\n", uidstr)
		err = c.SetConfigItem("lxc.id_map", uidstr)
		if err != nil {
			return InternalError(err)
		}
		gidstr := fmt.Sprintf("g 0 %d %d\n", d.id_map.Gidmin, d.id_map.Gidrange)
		err = c.SetConfigItem("lxc.id_map", gidstr)
		if err != nil {
			return InternalError(err)
		}
	}

	/*
	 * Actually create the container
	 */
	return AsyncResponse(func() error { return c.Create(opts) }, nil)
}

var containersCmd = Command{"containers", false, false, nil, nil, containersPost, nil}

func containerGet(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return InternalError(err)
	}

	if !c.Defined() {
		return NotFound
	}

	return SyncResponse(true, lxd.CtoD(c))
}

func containerDelete(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return InternalError(err)
	}

	if !c.Defined() {
		return NotFound
	}

	return AsyncResponse(c.Destroy, nil)
}

var containerCmd = Command{"containers/{name}", false, false, containerGet, nil, nil, containerDelete}

func containerStateGet(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return InternalError(err)
	}

	if !c.Defined() {
		return NotFound
	}

	return SyncResponse(true, lxd.CtoD(c).Status)
}

func containerStatePut(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]

	raw := lxd.Jmap{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return BadRequest(err)
	}

	action, err := raw.GetString("action")
	if err != nil {
		return BadRequest(err)
	}

	timeout, err := raw.GetInt("timeout")
	if err != nil {
		timeout = -1
	}

	force, err := raw.GetBool("force")
	if err != nil {
		force = false
	}

	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return InternalError(err)
	}

	if !c.Defined() {
		return NotFound
	}

	var do func() error
	switch action {
	case string(lxd.Start):
		do = c.Start
	case string(lxd.Stop):
		if timeout == 0 || force {
			do = c.Stop
		} else {
			do = func() error { return c.Shutdown(time.Duration(timeout)) }
		}
	case string(lxd.Restart):
		do = c.Reboot
	case string(lxd.Freeze):
		do = c.Freeze
	case string(lxd.Unfreeze):
		do = c.Unfreeze
	default:
		return BadRequest(fmt.Errorf("unknown action %s", action))
	}

	return AsyncResponse(do, nil)
}

var containerStateCmd = Command{"containers/{name}/state", false, false, containerStateGet, containerStatePut, nil, nil}

func containerFileHandler(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := lxc.NewContainer(name, d.lxcpath)
	if err != nil {
		return NotFound
	}

	targetPath := r.FormValue("path")
	if targetPath == "" {
		return BadRequest(fmt.Errorf("missing path argument"))
	}

	var rootfs string
	if c.Running() {
		rootfs = fmt.Sprintf("/proc/%d/root", c.InitPid())
	} else {
		/*
		 * TODO: We should ask LXC about whether or not this rootfs is a block
		 * device, and if it is, whether or not it is actually mounted.
		 */
		rootfs = c.ConfigItem("lxc.rootfs")[0]
	}

	/*
	 * Make sure someone didn't pass in ../../../etc/shadow or something.
	 */
	p := path.Clean(path.Join(rootfs, targetPath))
	if !strings.HasPrefix(p, path.Clean(rootfs)) {
		return BadRequest(fmt.Errorf("%s is not in the container's rootfs", p))
	}

	switch r.Method {
	case "GET":
		return containerFileGet(r, p)
	case "PUT":
		return containerFilePut(r, p)
	default:
		return NotFound
	}
}

type fileServe struct {
	req     *http.Request
	path    string
	fi      os.FileInfo
	content io.ReadSeeker
}

func (r *fileServe) Render(w http.ResponseWriter) error {
	/*
	 * Unfortunately, there's no portable way to do this:
	 * https://groups.google.com/forum/#!topic/golang-nuts/tGYjYyrwsGM
	 * https://groups.google.com/forum/#!topic/golang-nuts/ywS7xQYJkHY
	 */
	sb := r.fi.Sys().(*syscall.Stat_t)
	w.Header().Set("X-LXD-uid", strconv.FormatUint(uint64(sb.Uid), 10))
	w.Header().Set("X-LXD-gid", strconv.FormatUint(uint64(sb.Gid), 10))
	w.Header().Set("X-LXD-mode", fmt.Sprintf("%04o", r.fi.Mode()&os.ModePerm))

	http.ServeContent(w, r.req, r.path, r.fi.ModTime(), r.content)
	return nil
}

func containerFileGet(r *http.Request, path string) Response {
	f, err := os.Open(path)
	if err != nil {
		return SmartError(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return InternalError(err)
	}

	return &fileServe{r, filepath.Base(path), fi, f}
}

func containerFilePut(r *http.Request, p string) Response {

	uid, gid, mode, err := lxd.ParseLXDFileHeaders(r.Header)
	if err != nil {
		return BadRequest(err)
	}

	err = os.MkdirAll(path.Dir(p), mode)
	if err != nil {
		return SmartError(err)
	}

	f, err := os.Create(p)
	if err != nil {
		return SmartError(err)
	}
	defer f.Close()

	err = f.Chmod(mode)
	if err != nil {
		return SmartError(err)
	}

	err = f.Chown(uid, gid)
	if err != nil {
		return SmartError(err)
	}

	_, err = io.Copy(f, r.Body)
	if err != nil {
		return InternalError(err)
	}

	return EmptySyncResponse
}

var containerFileCmd = Command{"containers/{name}/files", false, false, containerFileHandler, containerFileHandler, nil, nil}
