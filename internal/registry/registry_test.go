package registry

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Registry tests only exercise the embedded table; do not depend on IANA 网络。
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

func TestFindBestTLDUsesLongestSecondLevelSuffix(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("加载内置 registry 配置失败: %v", err)
	}
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "example.com.cn", want: "com.cn"},
		{name: "example.net.cn", want: "net.cn"},
		{name: "example.org.cn", want: "org.cn"},
		{name: "example.cn", want: "cn"},
	} {
		if got := FindBestTLD(test.name); got != test.want {
			t.Errorf("FindBestTLD(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}
