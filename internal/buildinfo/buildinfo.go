package buildinfo

// 这些变量由 Dockerfile 的 ldflags 注入；本地 go test 使用安全默认值。
var (
	Version   = "0.4.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	Dirty     = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Dirty     string `json:"dirty"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime, Dirty: Dirty}
}
