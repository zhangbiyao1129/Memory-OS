package webdeploy

import (
	"os"
	"strings"
	"testing"
)

func TestWebDockerfileServesGeneratedNuxtStaticSite(t *testing.T) {
	content, err := os.ReadFile("../../deploy/Dockerfile.web")
	if err != nil {
		t.Fatalf("read Dockerfile.web: %v", err)
	}
	dockerfile := string(content)

	if !strings.Contains(dockerfile, "RUN npm run generate") {
		t.Fatalf("Dockerfile.web must run nuxt generate so the SPA index.html is emitted")
	}
	if !strings.Contains(dockerfile, "ARG NUXT_PUBLIC_API_BASE=http://localhost:18081") {
		t.Fatalf("Dockerfile.web must accept NUXT_PUBLIC_API_BASE as a build argument")
	}
	if !strings.Contains(dockerfile, "ENV NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE}") {
		t.Fatalf("Dockerfile.web must expose NUXT_PUBLIC_API_BASE to nuxt generate")
	}
	if !strings.Contains(dockerfile, "RUN rm -rf /usr/share/nginx/html/*") {
		t.Fatalf("Dockerfile.web must remove nginx default html before copying Nuxt output")
	}
	if !strings.Contains(dockerfile, "COPY --from=build /src/web/.output/public/ /usr/share/nginx/html/") {
		t.Fatalf("Dockerfile.web must copy generated Nuxt public contents into nginx html root")
	}
}

func TestComposePassesExternalAPIBaseForT480WebBuild(t *testing.T) {
	baseCompose, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	t480Compose, err := os.ReadFile("../../deploy/docker-compose.t480.yml")
	if err != nil {
		t.Fatalf("read docker-compose.t480.yml: %v", err)
	}

	if !strings.Contains(string(baseCompose), "NUXT_PUBLIC_API_BASE: ${NUXT_PUBLIC_API_BASE:-http://localhost:18081}") {
		t.Fatalf("base compose must pass NUXT_PUBLIC_API_BASE to the web image build")
	}
	if !strings.Contains(string(t480Compose), "NUXT_PUBLIC_API_BASE: ${NUXT_PUBLIC_API_BASE:-http://ddns.08121.top:18081}") {
		t.Fatalf("T480 compose must default the generated web app API base to the external API URL")
	}
}
