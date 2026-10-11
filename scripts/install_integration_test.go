//go:build integration

// Package scripts tests install.sh, the installer published with each release, in a Debian container against a fake
// release server.
package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
)

// trixie is Debian 13 with curl, the base of current Raspberry Pi OS. Its /bin/sh is dash, as on the Pi.
const trixie = "buildpack-deps:trixie-curl@sha256:3dd78c060b4d7c97b426d0160f7d8876c84fbf2fc36e651e9c75620ac9a7da24"

const (
	binPath    = "/usr/local/bin/solix-mqtt-bridge"
	unitPath   = "/etc/systemd/system/solix-mqtt-bridge.service"
	configPath = "/etc/solix-mqtt-bridge.env"
	shimState  = "/var/lib/systemctl-shim"

	latest   = "9.9.9"
	previous = "9.9.8"
	badSum   = "9.9.7"
	noSum    = "9.9.6"

	defaultPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	shimPath    = "/opt/shims:" + defaultPath

	customConfig = "SOLARBANK_ADDR=192.0.2.10:502\n"
)

var archs = []string{"linux_amd64", "linux_arm64", "linux_armv7"}

func TestFreshInstallLeavesTheServiceDisabledAndStopped(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	out := succeeds(t)(p.install(t, shimPath, "", latest))
	assertSteps(t, out, "solix-mqtt-bridge installer", "Checking the system", "Finding the release: "+latest,
		"Downloading", "Verifying the checksum: OK", "Installing the binary", "Service file: installed",
		"Configuration: created")
	assertContains(t, out, "sudo systemctl enable --now solix-mqtt-bridge")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output has ANSI escape codes although stdout isn't a terminal:\n%q", out)
	}
	p.assertVersion(t, latest)
	for path, want := range map[string]string{binPath: "755", unitPath: "644", configPath: "600"} {
		if got := strings.TrimSpace(p.must(t, "stat -c %a "+path)); got != want {
			t.Errorf("mode of %s = %s, want %s", path, got, want)
		}
	}
	p.assertFile(t, unitPath, read(t, "../deploy/examples/solix-mqtt-bridge.service"))
	p.assertFile(t, configPath, read(t, "../deploy/examples/solix-mqtt-bridge.env"))
	p.assertChanges(t, "daemon-reload")
}

func TestNoVersionInstallsTheLatestRelease(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	out := succeeds(t)(p.install(t, shimPath, ""))
	assertContains(t, out, "latest is "+latest)
	p.assertVersion(t, latest)
}

func TestUnknownVersionChangesNothing(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	assertContains(t, fails(t)(p.install(t, shimPath, "", "1.2.3")), "not found")
	p.assertAbsent(t, binPath, unitPath, configPath)
}

func TestUpdateRestartsAnEnabledServiceAndReplacesAChangedUnit(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	p.configured(t, previous)
	p.must(t, "echo '# local edit' >> "+unitPath)
	out := succeeds(t)(p.install(t, shimPath, "", latest))
	assertSteps(t, out, "solix-mqtt-bridge installer", "Checking the system", "Finding the release: "+latest,
		"Downloading", "Verifying the checksum: OK", "Installing the binary", "Service file: replaced",
		"Configuration: "+configPath+" kept", "Restarting solix-mqtt-bridge", "Waiting for the service: running")
	assertContains(t, out, previous+" -> "+latest)
	assertContains(t, out, "sudo systemctl edit solix-mqtt-bridge")
	p.assertFile(t, configPath, []byte(customConfig))
	p.assertFile(t, unitPath, read(t, "../deploy/examples/solix-mqtt-bridge.service"))
	p.assertChanges(t, "daemon-reload", "restart solix-mqtt-bridge")
}

func TestUpdateLeavesADisabledStoppedServiceAlone(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	p.configured(t, previous)
	p.must(t, "systemctl disable --now solix-mqtt-bridge && rm -f "+shimState+"/calls")
	assertContains(t, succeeds(t)(p.install(t, shimPath, "", latest)), "Service file: unchanged")
	p.assertVersion(t, latest)
	p.assertChanges(t)
}

func TestRerunWithNothingChangedDoesntRestart(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	p.configured(t, latest)
	assertContains(t, succeeds(t)(p.install(t, shimPath, "", latest)), "Already up to date ("+latest+")")
	p.assertChanges(t)
}

func TestFailedRestartShowsTheLogAndHowToRollBack(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	p.configured(t, previous)
	p.must(t, "echo failed > "+shimState+"/start-result")
	out := fails(t)(p.install(t, shimPath, "", latest))
	assertContains(t, out, "Waiting for the service: failed")
	assertContains(t, out, "shim journal line 3")
	assertContains(t, out, "sudo sh -s -- "+previous)
}

func TestChecksumMismatchChangesNothing(t *testing.T) {
	t.Parallel()
	assertRefused(t, badSum, "checksum mismatch")
}

func TestArchiveMissingFromTheChecksumsChangesNothing(t *testing.T) {
	t.Parallel()
	assertRefused(t, noSum, "not listed")
}

// assertRefused installs version over a configured, running previous release and expects the installer to stop
// before changing anything.
func assertRefused(t *testing.T, version, want string) {
	t.Helper()
	p := startPi(t)
	p.configured(t, previous)
	assertContains(t, fails(t)(p.install(t, shimPath, "", version)), want)
	p.assertVersion(t, previous)
	p.assertFile(t, configPath, []byte(customConfig))
	p.assertChanges(t)
}

func TestUnsupportedArchitectureIsNamed(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	assertContains(t, fails(t)(p.install(t, "/opt/armv6l:"+shimPath, "", latest)), "armv6l is not supported")
	p.assertAbsent(t, binPath)
}

func TestNotRootAsksForSudo(t *testing.T) {
	t.Parallel()
	p := startPi(t)
	assertContains(t, fails(t)(p.install(t, shimPath, "nobody", latest)), "sudo")
	p.assertAbsent(t, binPath)
}

// succeeds and fails check an installer run's exit code and pass its output on.
func succeeds(t *testing.T) func(int, string) string {
	t.Helper()
	return func(code int, out string) string {
		t.Helper()
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; output:\n%s", code, out)
		}
		return out
	}
}

func fails(t *testing.T) func(int, string) string {
	t.Helper()
	return func(code int, out string) string {
		t.Helper()
		if code == 0 {
			t.Fatalf("exit code = 0, want a failure; output:\n%s", out)
		}
		return out
	}
}

// pi is a Debian container standing in for a Raspberry Pi, with systemctl and journalctl shims.
type pi struct {
	ctr *testcontainers.DockerContainer
	url string
}

func startPi(t *testing.T) *pi {
	t.Helper()
	port := serveReleases(t, buildReleases(t))
	ctr, err := testcontainers.Run(t.Context(), trixie,
		testcontainers.WithCmd("sleep", "infinity"),
		testcontainers.WithHostPortAccess(port),
		testcontainers.WithFiles(
			executable("testdata/shims/systemctl", "/opt/shims/systemctl"),
			executable("testdata/shims/journalctl", "/opt/shims/journalctl"),
			executable("testdata/armv6l/uname", "/opt/armv6l/uname"),
		))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	p := &pi{ctr: ctr, url: "http://" + testcontainers.HostInternal + ":" + strconv.Itoa(port) + "/releases"}
	p.must(t, "mkdir -p /run/systemd/system /etc/systemd/system "+shimState)
	return p
}

func executable(host, container string) testcontainers.ContainerFile {
	return testcontainers.ContainerFile{HostFilePath: host, ContainerFilePath: container, FileMode: 0o755}
}

func (p *pi) exec(t *testing.T, path, user, script string) (int, string) {
	t.Helper()
	opts := []tcexec.ProcessOption{
		tcexec.Multiplexed(),
		tcexec.WithEnv([]string{"PATH=" + path, "SOLIX_RELEASES_URL=" + p.url}),
	}
	if user != "" {
		opts = append(opts, tcexec.WithUser(user))
	}
	code, r, err := p.ctr.Exec(t.Context(), []string{"sh", "-c", script}, opts...)
	if err != nil {
		t.Fatalf("exec %q: %v", script, err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output of %q: %v", script, err)
	}
	return code, string(out)
}

func (p *pi) must(t *testing.T, script string) string {
	t.Helper()
	code, out := p.exec(t, shimPath, "", script)
	if code != 0 {
		t.Fatalf("%q exited with %d:\n%s", script, code, out)
	}
	return out
}

// install runs the installer the way the docs do, piped from the release server into sh, and checks that it left no
// temporary files behind.
func (p *pi) install(t *testing.T, path, user string, args ...string) (int, string) {
	t.Helper()
	before := p.must(t, "ls -A /tmp")
	code, out := p.exec(t, path, user,
		`curl -fsSL "$SOLIX_RELEASES_URL/latest/download/install.sh" | sh -s -- `+strings.Join(args, " "))
	if after := p.must(t, "ls -A /tmp"); after != before {
		t.Errorf("/tmp after the run = %q, want %q (no leftovers)", after, before)
	}
	return code, out
}

// configured installs version as the user would after the first install: edited config, service enabled and running.
func (p *pi) configured(t *testing.T, version string) {
	t.Helper()
	if code, out := p.install(t, shimPath, "", version); code != 0 {
		t.Fatalf("install %s: exit code %d:\n%s", version, code, out)
	}
	p.must(t, "printf '"+strings.TrimSuffix(customConfig, "\n")+"\\n' > "+configPath+
		" && systemctl enable --now solix-mqtt-bridge && rm -f "+shimState+"/calls")
}

func (p *pi) changes(t *testing.T) []string {
	t.Helper()
	var got []string
	for line := range strings.Lines(p.must(t, "cat "+shimState+"/calls 2>/dev/null || true")) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "is-active") && !strings.HasPrefix(line, "is-enabled") {
			got = append(got, line)
		}
	}
	return got
}

func (p *pi) assertVersion(t *testing.T, version string) {
	t.Helper()
	assertContains(t, p.must(t, binPath+" -version"), "solix-mqtt-bridge "+version+" ")
}

// assertChanges checks the systemctl calls that change something, ignoring state queries.
func (p *pi) assertChanges(t *testing.T, want ...string) {
	t.Helper()
	if got := p.changes(t); !slices.Equal(got, want) {
		t.Errorf("systemctl changes = %q, want %q", got, want)
	}
}

func (p *pi) assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := p.must(t, "cat "+path); got != string(want) {
		t.Errorf("%s =\n%s\nwant\n%s", path, got, want)
	}
}

func (p *pi) assertAbsent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if code, _ := p.exec(t, shimPath, "", "test -e "+path); code == 0 {
			t.Errorf("%s exists, want nothing installed", path)
		}
	}
}

func assertContains(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// assertSteps checks the installer's "==> " step lines, in order, by prefix.
func assertSteps(t *testing.T, out string, want ...string) {
	t.Helper()
	var steps []string
	for line := range strings.Lines(out) {
		if step, ok := strings.CutPrefix(strings.TrimSpace(line), "==> "); ok {
			steps = append(steps, step)
		}
	}
	if len(steps) != len(want) {
		t.Fatalf("steps =\n%s\nwant prefixes %q", strings.Join(steps, "\n"), want)
	}
	for i := range want {
		if !strings.HasPrefix(steps[i], want[i]) {
			t.Errorf("step %d = %q, want prefix %q", i+1, steps[i], want[i])
		}
	}
}

// releases maps "v<version>/<asset>" to the asset's content, like GitHub's /releases/download/ URLs.
type releases map[string][]byte

// buildReleases makes four releases with the real install.sh and deploy/examples files: latest and previous are
// valid, badSum's checksums don't match, and noSum's checksums file doesn't list the archives.
func buildReleases(t *testing.T) releases {
	t.Helper()
	script := read(t, "install.sh")
	r := releases{}
	for _, version := range []string{latest, previous, badSum, noSum} {
		var sums strings.Builder
		for _, arch := range archs {
			name := "solix-mqtt-bridge_" + version + "_" + arch + ".tar.gz"
			archive := buildArchive(t, version)
			r["v"+version+"/"+name] = archive
			sum := sha256hex(archive)
			if version == badSum {
				sum = strings.Repeat("0", len(sum))
			}
			if version != noSum {
				sums.WriteString(sum + "  " + name + "\n")
			}
		}
		sums.WriteString(sha256hex(script) + "  install.sh\n")
		r["v"+version+"/solix-mqtt-bridge_"+version+"_checksums.txt"] = []byte(sums.String())
		r["v"+version+"/install.sh"] = script
	}
	return r
}

// buildArchive mirrors goreleaser's archive layout. The binary is a stub that only answers -version.
func buildArchive(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, mode int64, body []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	add("solix-mqtt-bridge", 0o755, []byte("#!/bin/sh\necho 'solix-mqtt-bridge "+version+" (stub, test)'\n"))
	for _, name := range []string{"docker-compose.yml", "solix-mqtt-bridge.env", "solix-mqtt-bridge.service"} {
		add("deploy/examples/"+name, 0o644, read(t, "../deploy/examples/"+name))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveReleases answers like github.com/<owner>/<repo>/releases, including the "latest" redirects, and returns the
// local port.
func serveReleases(t *testing.T, r releases) int {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /releases/latest", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/releases/tag/v"+latest, http.StatusFound)
	})
	mux.HandleFunc("GET /releases/latest/download/{name}", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/releases/download/v"+latest+"/"+req.PathValue("name"), http.StatusFound)
	})
	mux.HandleFunc("GET /releases/tag/{tag}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "release page")
	})
	mux.HandleFunc("GET /releases/download/{tag}/{name}", func(w http.ResponseWriter, req *http.Request) {
		body, ok := r[req.PathValue("tag")+"/"+req.PathValue("name")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
