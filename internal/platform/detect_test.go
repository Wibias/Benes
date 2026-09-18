package platform

import "testing"

func TestClassifyServiceManagerRecognizesLaunchdSystemdAndWindows(t *testing.T) {
	cases := []struct {
		name string
		have map[string]bool
		want ServiceManager
	}{
		{
			name: "launchd",
			have: map[string]bool{"/bin/launchctl": true, "/System/Library/LaunchDaemons": true},
			want: ServiceManagerLaunchd,
		},
		{
			name: "systemd",
			have: map[string]bool{"/run/systemd/system": true, "/run/dbus/system_bus_socket": true},
			want: ServiceManagerSystemd,
		},
		{
			name: "windows",
			have: map[string]bool{`C:\Windows\System32\services.exe`: true},
			want: ServiceManagerWindowsSCM,
		},
		{
			name: "scheduler",
			have: map[string]bool{`C:\Windows\System32\schtasks.exe`: true},
			want: ServiceManagerScheduler,
		},
		{
			name: "systemctl without runtime is unavailable",
			have: map[string]bool{"/bin/systemctl": true},
			want: ServiceManagerUnavailable,
		},
		{
			name: "nothing",
			have: map[string]bool{},
			want: ServiceManagerUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyServiceManager(func(path string) bool { return tc.have[path] })
			if got != tc.want {
				t.Fatalf("got=%s want=%s", got, tc.want)
			}
		})
	}
}
