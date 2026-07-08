package workspace

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

type Identity struct {
	CWD         string `json:"cwd,omitempty"`
	GitRoot     string `json:"git_root,omitempty"`
	GitRemote   string `json:"git_remote,omitempty"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitCommit   string `json:"git_commit,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	SourceKey   string `json:"source_key,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
}

func Resolve(identity Identity) (Identity, error) {
	if sourceKey := strings.Trim(strings.ToLower(identity.SourceKey), "/"); sourceKey != "" {
		sourceType := strings.TrimSpace(identity.SourceType)
		if sourceType == "" {
			sourceType = "manual"
		}
		identity.SourceType = sourceType
		identity.SourceKey = sourceKey
		if strings.TrimSpace(identity.ProjectName) == "" {
			identity.ProjectName = projectName(repoNameFromSourceKey(sourceKey))
		}
		if strings.TrimSpace(identity.ProjectSlug) == "" {
			identity.ProjectSlug = slug(sourceType + "/" + sourceKey)
		}
		return identity, nil
	}
	sourceKey, err := NormalizeGitRemote(identity.GitRemote)
	if err == nil {
		identity.SourceType = "git"
		identity.SourceKey = sourceKey
		repoName := repoNameFromSourceKey(sourceKey)
		identity.ProjectName = projectName(repoName)
		identity.ProjectSlug = slug(sourceKey)
		return identity, nil
	}
	if strings.TrimSpace(identity.GitRemote) != "" {
		return Identity{}, err
	}
	return resolveLocalOrInbox(identity), nil
}

func resolveLocalOrInbox(identity Identity) Identity {
	localPath := strings.TrimSpace(identity.GitRoot)
	if localPath == "" {
		localPath = strings.TrimSpace(identity.CWD)
	}
	if localPath == "" {
		identity.SourceType = "inbox"
		identity.SourceKey = "inbox/general"
		identity.ProjectName = "Inbox"
		identity.ProjectSlug = "inbox-general"
		return identity
	}
	sourceKey := "local/" + slug(localPath)
	identity.SourceType = "local"
	identity.SourceKey = sourceKey
	identity.ProjectName = projectName(repoNameFromSourceKey(localPath))
	identity.ProjectSlug = slug(sourceKey)
	return identity
}

func NormalizeGitRemote(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", errors.New("git remote is required")
	}
	host := ""
	path := ""
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return "", err
		}
		host = parsed.Hostname()
		path = parsed.EscapedPath()
	} else if matches := scpLikeRemote.FindStringSubmatch(remote); len(matches) == 3 {
		host = matches[1]
		path = matches[2]
	} else {
		parts := strings.SplitN(remote, "/", 2)
		if len(parts) == 2 {
			host = parts[0]
			path = parts[1]
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.ToLower(path)
	if host == "" || path == "" {
		return "", errors.New("git remote format is invalid")
	}
	return host + "/" + path, nil
}

var scpLikeRemote = regexp.MustCompile(`^[^@]+@([^:]+):(.+)$`)

func repoNameFromSourceKey(sourceKey string) string {
	parts := strings.Split(strings.Trim(sourceKey, "/"), "/")
	if len(parts) == 0 {
		return "workspace"
	}
	return strings.TrimSuffix(parts[len(parts)-1], ".git")
}

func projectName(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "Workspace"
	}
	words := strings.FieldsFunc(repo, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		if len(runes) <= 3 {
			words[i] = strings.ToUpper(string(runes))
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	if len(words) == 0 {
		return "Workspace"
	}
	return strings.Join(words, " ")
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	builder := strings.Builder{}
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "workspace"
	}
	return result
}
