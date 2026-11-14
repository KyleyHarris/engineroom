// Package scripts holds the tests of the repository's shell scripts that can be run without a
// build: install-app.sh, which puts the menu bar app in place signed (docs/decisions.md D-038).
package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The identity the stand-in security lists, in find-identity's own format.
const identity = "0123456789ABCDEF0123456789ABCDEF01234567"

// machine is a Mac as install-app.sh sees it: stand-ins for security and codesign on the PATH,
// an apps folder, and a built binary and Info.plist template to install.
type machine struct {
	t       *testing.T
	apps    string
	binary  string
	plist   string
	log     string
	env     []string
	bin     string
	lastOut string
}

// security and codesign are stand-ins because the real ones need a developer's identity and an
// open keychain, which a test machine (and CI) does not have; the script under test, its moves
// and its failure handling are real.
//
// newMachine makes a machine. hasIdentity says whether security lists an Apple Development
// identity; failIdentity and failAdHoc make codesign fail when signing with the identity, or ad hoc.
func newMachine(t *testing.T, hasIdentity, failIdentity, failAdHoc bool) *machine {
	t.Helper()
	root := t.TempDir()
	m := &machine{t: t, apps: filepath.Join(root, "Applications"), binary: filepath.Join(root, "EngineRoomMenu"),
		plist: filepath.Join(root, "Info.plist"), log: filepath.Join(root, "codesign.log"), bin: filepath.Join(root, "bin")}
	must(t, os.MkdirAll(m.bin, 0o755))
	must(t, os.WriteFile(m.binary, []byte("built binary"), 0o755))
	must(t, os.WriteFile(m.plist, []byte("<string>VERSION</string>\n"), 0o644))
	security := "#!/bin/sh\n"
	if hasIdentity {
		security += `echo '  1) ` + identity + ` "Apple Development: Developer (TEAM123456)"'` + "\necho '     1 valid identities found'\n"
	} else {
		security += "echo '     0 valid identities found'\n"
	}
	// codesign records each call, then signs as asked: the bundle's signature file names the signer,
	// as a real signature would, so a test can read which signature the installed app carries.
	codesign := `#!/bin/sh
echo "$*" >> "` + m.log + `"
signer="$3"; bundle="$4"
if [ "$signer" = "-" ] && [ "${FAIL_ADHOC:-}" = 1 ]; then echo "codesign: ad hoc failed" >&2; exit 1; fi
if [ "$signer" != "-" ] && [ "${FAIL_IDENTITY:-}" = 1 ]; then echo "$bundle: errSecInternalComponent" >&2; exit 1; fi
mkdir -p "$bundle/Contents/_CodeSignature" && echo "$signer" > "$bundle/Contents/_CodeSignature/signer"
`
	must(t, os.WriteFile(filepath.Join(m.bin, "security"), []byte(security), 0o755))
	must(t, os.WriteFile(filepath.Join(m.bin, "codesign"), []byte(codesign), 0o755))
	m.env = []string{"PATH=" + m.bin + ":/usr/bin:/bin", "HOME=" + root}
	if failIdentity {
		m.env = append(m.env, "FAIL_IDENTITY=1")
	}
	if failAdHoc {
		m.env = append(m.env, "FAIL_ADHOC=1")
	}
	return m
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// install runs install-app.sh as install.sh does, and returns whether it succeeded.
func (m *machine) install(version string) bool {
	m.t.Helper()
	cmd := exec.Command("bash", "install-app.sh", m.binary, m.plist, version, m.apps)
	cmd.Env = m.env
	out, err := cmd.CombinedOutput()
	m.lastOut = string(out)
	return err == nil
}

func (m *machine) app() string { return filepath.Join(m.apps, "engineroom.app") }

func (m *machine) read(rel string) string {
	m.t.Helper()
	b, err := os.ReadFile(filepath.Join(m.app(), rel))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// installedBefore puts an app in place as an earlier install would have, signed with signer.
func (m *machine) installedBefore(version, signer string) {
	m.t.Helper()
	must(m.t, os.MkdirAll(filepath.Join(m.app(), "Contents/MacOS"), 0o755))
	must(m.t, os.MkdirAll(filepath.Join(m.app(), "Contents/_CodeSignature"), 0o755))
	must(m.t, os.WriteFile(filepath.Join(m.app(), "Contents/MacOS/engineroom-menu"), []byte("earlier binary"), 0o755))
	must(m.t, os.WriteFile(filepath.Join(m.app(), "Contents/Info.plist"), []byte("<string>"+version+"</string>\n"), 0o644))
	must(m.t, os.WriteFile(filepath.Join(m.app(), "Contents/_CodeSignature/signer"), []byte(signer+"\n"), 0o644))
}

// onlyTheApp fails unless the apps folder holds the installed app and nothing else: no copy
// being assembled and no earlier app moved aside is left behind.
func (m *machine) onlyTheApp() {
	m.t.Helper()
	entries, err := os.ReadDir(m.apps)
	must(m.t, err)
	if len(entries) != 1 || entries[0].Name() != "engineroom.app" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		m.t.Fatalf("the apps folder holds %v, want only engineroom.app", names)
	}
}

func (m *machine) signedAdHoc() bool {
	b, _ := os.ReadFile(m.log)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "--force --sign - ") {
			return true
		}
	}
	return false
}

// On a Mac with an Apple Development identity the app is installed signed with it, with the
// built binary and its version, and nothing is left beside it. It fails if the identity is not
// used, the app is signed ad hoc as well, or the binary or version is not the one built.
func TestTheAppIsSignedWithTheIdentity(t *testing.T) {
	m := newMachine(t, true, false, false)
	m.installedBefore("v1", identity)
	if !m.install("v2") {
		t.Fatalf("the install failed:\n%s", m.lastOut)
	}
	if got := m.read("Contents/_CodeSignature/signer"); got != identity {
		t.Fatalf("the app is signed by %q, want the identity", got)
	}
	if m.signedAdHoc() {
		t.Fatal("the app was signed ad hoc on a Mac with an identity")
	}
	if got := m.read("Contents/MacOS/engineroom-menu"); got != "built binary" {
		t.Fatalf("the app's binary is %q, want the one built", got)
	}
	if got := m.read("Contents/Info.plist"); got != "<string>v2</string>" {
		t.Fatalf("the app's Info.plist is %q, want version v2", got)
	}
	m.onlyTheApp()
}

// Where signing with the identity fails, nothing is installed: the app already there keeps its
// binary, version and signature, it is never signed ad hoc, and the output says how to let
// codesign use the key. It fails if the run succeeds, falls back to ad hoc, touches the installed
// app, leaves its copy behind, or does not name the fix.
func TestASigningFailureLeavesTheInstalledAppAsItWas(t *testing.T) {
	m := newMachine(t, true, true, false)
	m.installedBefore("v1", identity)
	if m.install("v2") {
		t.Fatalf("the install succeeded although signing failed:\n%s", m.lastOut)
	}
	if m.signedAdHoc() {
		t.Fatal("the app was signed ad hoc after the identity failed")
	}
	if got := m.read("Contents/_CodeSignature/signer"); got != identity {
		t.Fatalf("the installed app's signature is now %q, want it unchanged", got)
	}
	if got := m.read("Contents/MacOS/engineroom-menu"); got != "earlier binary" {
		t.Fatalf("the installed app's binary is now %q, want it unchanged", got)
	}
	if got := m.read("Contents/Info.plist"); got != "<string>v1</string>" {
		t.Fatalf("the installed app's version is now %q, want it unchanged", got)
	}
	for _, want := range []string{"was not installed", "unchanged", "security set-key-partition-list -S apple-tool:,apple:,codesign:"} {
		if !strings.Contains(m.lastOut, want) {
			t.Fatalf("the output does not say %q:\n%s", want, m.lastOut)
		}
	}
	m.onlyTheApp()
}

// A signing failure on a Mac with no app installed yet installs nothing.
func TestASigningFailureOnAFirstInstallInstallsNothing(t *testing.T) {
	m := newMachine(t, true, true, false)
	if m.install("v1") {
		t.Fatalf("the install succeeded although signing failed:\n%s", m.lastOut)
	}
	entries, err := os.ReadDir(m.apps)
	must(t, err)
	if len(entries) != 0 {
		t.Fatalf("the apps folder holds %d entries after a failed first install, want none", len(entries))
	}
}

// A Mac with no identity gets an app signed ad hoc, and is told macOS will ask again for its
// permissions. It fails if the app is not installed or not signed ad hoc.
func TestWithNoIdentityTheAppIsSignedAdHoc(t *testing.T) {
	m := newMachine(t, false, false, false)
	if !m.install("v1") {
		t.Fatalf("the install failed:\n%s", m.lastOut)
	}
	if got := m.read("Contents/_CodeSignature/signer"); got != "-" {
		t.Fatalf("the app is signed by %q, want ad hoc", got)
	}
	if !strings.Contains(m.lastOut, "no Apple Development identity") {
		t.Fatalf("the output does not say the Mac has no identity:\n%s", m.lastOut)
	}
	m.onlyTheApp()
}

// Where ad hoc signing fails too, the installed app is left as it was.
func TestAnAdHocFailureLeavesTheInstalledAppAsItWas(t *testing.T) {
	m := newMachine(t, false, false, true)
	m.installedBefore("v1", "-")
	if m.install("v2") {
		t.Fatalf("the install succeeded although signing failed:\n%s", m.lastOut)
	}
	if got := m.read("Contents/Info.plist"); got != "<string>v1</string>" {
		t.Fatalf("the installed app's version is now %q, want it unchanged", got)
	}
	m.onlyTheApp()
}

// A run interrupted earlier leaves a copy being assembled and an app moved aside; the next run
// removes both and installs, and running it again installs the same app with nothing left over.
// It fails if a leftover blocks the install, survives it, or a second run differs.
func TestLeftoversAreClearedAndASecondRunIsTheSame(t *testing.T) {
	m := newMachine(t, true, false, false)
	m.installedBefore("v1", identity)
	for _, left := range []string{".engineroom.app.new", ".engineroom.app.old"} {
		must(t, os.MkdirAll(filepath.Join(m.apps, left, "Contents"), 0o755))
		must(t, os.WriteFile(filepath.Join(m.apps, left, "Contents/stale"), []byte("x"), 0o644))
	}
	for run := 1; run <= 2; run++ {
		if !m.install("v2") {
			t.Fatalf("run %d failed:\n%s", run, m.lastOut)
		}
		if got := m.read("Contents/Info.plist"); got != "<string>v2</string>" {
			t.Fatalf("run %d: the app's version is %q, want v2", run, got)
		}
		if got := m.read("Contents/stale"); got != "" {
			t.Fatalf("run %d: a leftover's file is in the installed app", run)
		}
		m.onlyTheApp()
	}
}

// Called with the wrong number of arguments, it says how to call it and changes nothing.
func TestWrongArgumentsAreRefused(t *testing.T) {
	m := newMachine(t, true, false, false)
	cmd := exec.Command("bash", "install-app.sh", m.binary)
	cmd.Env = m.env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err == nil || !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "usage:") {
		t.Fatalf("wrong arguments gave %v:\n%s", err, out)
	}
	if _, err := os.Stat(m.apps); !os.IsNotExist(err) {
		t.Fatal("the apps folder was made by a refused call")
	}
}

// install.sh runs install-app.sh as a program, so it must be executable in the checkout; the
// tests above run it with bash and would pass without. It fails if the file loses its mode.
func TestInstallAppIsExecutable(t *testing.T) {
	info, err := os.Stat("install-app.sh")
	must(t, err)
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("install-app.sh has mode %v; install.sh cannot run it", info.Mode().Perm())
	}
}
