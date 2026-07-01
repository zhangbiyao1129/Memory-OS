package verify_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyScriptRunsDeliveryGatesInOrder(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "verify.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("verify script must exist at %s: %v", scriptPath, err)
	}

	tempDir := t.TempDir()
	stepsPath := filepath.Join(tempDir, "steps.log")
	mark := func(name string) string {
		return "printf '%s\\n' " + name + " >> " + shellQuote(stepsPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"PREFLIGHT_CMD="+mark("preflight"),
		"GO_TEST_CMD="+mark("go-test"),
		"WEB_BUILD_CMD="+mark("web-build"),
		"NPM_AUDIT_CMD="+mark("npm-audit"),
		"SMOKE_CMD="+mark("smoke"),
		"BACKUP_DRY_RUN_CMD="+mark("backup-dry-run"),
		"RESTORE_DRY_RUN_CMD="+mark("restore-dry-run"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify script failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(stepsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	want := strings.Join([]string{"preflight", "go-test", "web-build", "npm-audit", "smoke", "backup-dry-run", "restore-dry-run"}, "\n")
	if got != want {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if !strings.Contains(string(output), "verify completed") {
		t.Fatalf("output should report completion, got: %s", output)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

## 部署前 preflight

`make preflight` 会检查 Docker、docker-compose、默认外部端口 `18080/18081/18082/18083` 和最小可用磁盘空间。部署前如果该命令失败，不应继续执行 `make dev-up`。

在已经启动 Memory OS compose 栈的服务器上，`make verify` 会以 `ALLOW_EXISTING_DEPLOYMENT=1` 运行 preflight，允许当前项目自己的容器占用 `18080/18081/18082/18083`；裸 `make preflight` 仍会严格拒绝任何端口占用。

## Secret scan

`make secret-scan` 会扫描可提交内容中的高风险密钥形态，包括 `sk-...`、`pk_...`、AWS access key、Slack token 和 private key block。占位符如 `replace-me` 和测试用红队字符串会被放行。

## Backup cron dry-run

`make verify` 会以 dry-run 模式输出每日备份 cron 行，证明备份计划任务可安装；真实安装必须设置 `DRY_RUN=0 CONFIRM_CRON_INSTALL=I_UNDERSTAND`。
