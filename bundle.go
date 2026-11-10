package main

import (
	"bytes"
	"encoding/xml"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// relaunchIntoUpdatedBundle replaces this process with the app bundle's
// binary when the bundle on disk is newer than the copy that is running.
//
// A bundle can be replaced while an older copy keeps running: installing
// from the disk image, or an update that another instance staged, leaves
// the process reporting the version it started with. The updater picks its
// delta by that version and applies it to the bundle on disk, so every
// check downloads a delta that cannot apply and then the whole archive.
// Starting the newer binary instead puts the two back in step.
func relaunchIntoUpdatedBundle() {
	if os.Getenv("MYGO_DEV") != "" || os.Getenv("GEXI_RELAUNCHED") != "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	onDisk := bundleVersion(exe)
	if onDisk == "" || !versionNewer(onDisk, Version) {
		return
	}
	log.Printf("gexi: the bundle is %s and this process is %s; starting the bundle", onDisk, Version)
	env := append(os.Environ(), "GEXI_RELAUNCHED=1")
	_ = unix.Exec(exe, append([]string{exe}, os.Args[1:]...), env)
}

// bundleVersion is the version the app bundle holding exe reports, or ""
// when there is no Info.plist to read.
func bundleVersion(exe string) string {
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	plist, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	return plistValue(plist, "CFBundleShortVersionString")
}

// plistValue is the string an XML plist gives for key.
func plistValue(plist []byte, key string) string {
	dec := xml.NewDecoder(bytes.NewReader(plist))
	lastKey := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		start, ok := tok.(xml.StartElement)
		if !ok || (start.Name.Local != "key" && start.Name.Local != "string") {
			continue
		}
		var value string
		if err := dec.DecodeElement(&value, &start); err != nil {
			return ""
		}
		if start.Name.Local == "string" {
			if lastKey == key {
				return value
			}
			lastKey = ""
			continue
		}
		lastKey = value
	}
}

// versionNewer reports whether version a comes after b, comparing numeric
// fields in order: 0.0.10 is newer than 0.0.9, and a release is newer than
// its own pre-releases.
func versionNewer(a, b string) bool {
	as, apre := splitVersion(a)
	bs, bpre := splitVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return x > y
		}
	}
	switch {
	case apre == bpre:
		return false
	case apre == "":
		return true
	case bpre == "":
		return false
	}
	return apre > bpre
}

// splitVersion splits a version into its numeric fields and its pre-release
// suffix, ignoring a build suffix.
func splitVersion(v string) ([]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	fields := make([]int, 0, 3)
	for _, field := range strings.Split(v, ".") {
		n, err := strconv.Atoi(field)
		if err != nil {
			n = 0
		}
		fields = append(fields, n)
	}
	return fields, pre
}
