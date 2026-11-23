package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSyncProviderSettingsStayLocal(t *testing.T) {
	for _, provider := range []SyncProvider{SyncGist, SyncWebDAV, SyncS3, SyncLocal} {
		t.Run(string(provider), func(t *testing.T) {
			t.Setenv("FINALLYSHELL_DIR", t.TempDir())
			s := Settings{Sync: SyncSettings{
				Provider: provider, LocalDir: t.TempDir(), GistID: "gist-id",
				WebDAVURL: "https://dav.example/", WebDAVUser: "local-user",
				S3Endpoint: "https://s3.example/", S3Bucket: "bucket", S3Key: "file",
				S3Region: "region", S3AccessKeyID: "key-id", Auto: true, Interval: 30, Passwords: true,
			}}
			if err := SaveSettings(s); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(LoadSettings().Sync, s.Sync) {
				t.Fatal("sync configuration did not persist locally")
			}
			if _, ok := SyncedSettings(s)["sync"]; ok {
				t.Fatal("sync configuration is in the upload")
			}
			// Even a foreign sync object in an incoming payload cannot
			// replace this computer's destination or automatic options.
			incoming := map[string]json.RawMessage{"sync": json.RawMessage(`{"provider":"local","localDir":"other-device"}`)}
			if !reflect.DeepEqual(WithSyncedSettings(s, incoming).Sync, s.Sync) {
				t.Fatal("incoming data replaced the local sync configuration")
			}
		})
	}
}

func TestLocalSyncDestinationIdentity(t *testing.T) {
	one := SyncSettings{Provider: SyncLocal, LocalDir: "one"}
	two := SyncSettings{Provider: SyncLocal, LocalDir: "two"}
	if one.Remote() == two.Remote() || one.Remote() == "" {
		t.Fatal("different folders share a sync base")
	}
}

func TestLocalSyncRelativeDestinationIdentity(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	s := SyncSettings{Provider: SyncLocal, LocalDir: "sync"}
	t.Chdir(first)
	one := s.Remote()
	t.Chdir(second)
	if one == s.Remote() {
		t.Fatal("restarting in another working directory reused the old destination's merge base")
	}
	abs := SyncSettings{Provider: SyncLocal, LocalDir: filepath.Join(second, "sync")}
	if s.Remote() != abs.Remote() {
		t.Fatal("relative and absolute names of the same folder use different sync bases")
	}
}
