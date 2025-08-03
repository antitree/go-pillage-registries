#!/bin/bash
set -euo pipefail

echo "📁 Setting up demo materials directory..."
mkdir -p demo-materials
cd demo-materials

echo "🔐 Generating fake cosign signing key..."
COSIGN_PASSWORD="" cosign generate-key-pair
mv cosign.key signing.key
mv cosign.pub signing.pub

echo "🐳 Creating fake Docker config.json (docker.config.json)..."
cat > docker.config.json <<EOF
{
  "auths": {
    "https://index.docker.io/v1/": {
      "auth": "ZmFrZXVzZXI6ZmFrZXBhc3N3b3Jk"
    }
  }
}
EOF

echo "🐙 Creating fake Git repo..."
mkdir -p fake-repo && cd fake-repo
cat > Dockerfile <<EOF
FROM busybox
CMD ["echo", "hello from fake image"]
EOF
git init >/dev/null
git config user.email "demo@example.com"
git config user.name "Demo User"
git add Dockerfile
git commit -m "Initial commit" >/dev/null
cd ..

echo "✅ Demo materials ready in: $(pwd)"
echo ""
tree .

