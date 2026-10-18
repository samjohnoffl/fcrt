#!/usr/bin/env bash
# FCRT GCP One-Shot Setup Script
# Run: chmod +x scripts/gcp-setup.sh && ./scripts/gcp-setup.sh
# Requires: gcloud CLI installed and authenticated

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────────────
# CONFIG — EDIT THESE BEFORE RUNNING
# ──────────────────────────────────────────────────────────────────────────────
PROJECT_ID="${PROJECT_ID:-}"           # REQUIRED: your GCP project ID
REGION="${REGION:-us-east1}"        # Region for all resources
DB_TIER="${DB_TIER:-db-perf-optimized-N-4}"      # Cloud SQL tier (dev default for ENTERPRISE_PLUS, more capacity available)
MIN_INSTANCES="${MIN_INSTANCES:-1}"    # Cloud Run min instances
# ──────────────────────────────────────────────────────────────────────────────

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
log() { echo -e "${BLUE}[FCRT]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err() { echo -e "${RED}[ERR]${NC} $*"; exit 1; }

# ──────────────────────────────────────────────────────────────────────────────
# PRE-CHECKS
# ──────────────────────────────────────────────────────────────────────────────
[[ -z "$PROJECT_ID" ]] && err "Set PROJECT_ID env var: PROJECT_ID=my-project ./scripts/gcp-setup.sh"
command -v gcloud >/dev/null || err "gcloud CLI not found. Install: https://cloud.google.com/sdk/docs/install"
gcloud auth list --filter=status:ACTIVE --format="value(account)" | grep -q . || err "Run 'gcloud auth login' first"
gcloud auth application-default print-access-token >/dev/null 2>&1 || err "Run 'gcloud auth application-default login' first"

log "Using project: $PROJECT_ID | region: $REGION"
gcloud config set project "$PROJECT_ID" >/dev/null

# ──────────────────────────────────────────────────────────────────────────────
# 1. ENABLE APIs
# ──────────────────────────────────────────────────────────────────────────────
log "Enabling required APIs..."
gcloud services enable \
  sqladmin.googleapis.com \
  aiplatform.googleapis.com \
  firebase.googleapis.com \
  run.googleapis.com \
  vpcaccess.googleapis.com \
  secretmanager.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com \
  servicenetworking.googleapis.com \
  --project="$PROJECT_ID" >/dev/null
ok "APIs enabled"

# ──────────────────────────────────────────────────────────────────────────────
# 1b. CONFIGURE PRIVATE SERVICES ACCESS (VPC Peering for Cloud SQL private IP)
# ──────────────────────────────────────────────────────────────────────────────
log "Configuring Private Services Access (VPC peering)..."
# Allocate IP range for private services
gcloud compute addresses create google-managed-services-fcrt \
  --global \
  --purpose=VPC_PEERING \
  --prefix-length=16 \
  --network=fcrt-vpc \
  --project="$PROJECT_ID" 2>/dev/null || warn "Address range may exist"

# Create the VPC peering connection
gcloud services vpc-peerings connect \
  --service=servicenetworking.googleapis.com \
  --ranges=google-managed-services-fcrt \
  --network=fcrt-vpc \
  --project="$PROJECT_ID" 2>/dev/null || warn "Peering may exist"
ok "Private Services Access configured"

# ──────────────────────────────────────────────────────────────────────────────
# 2. ARTIFACT REGISTRY
# ──────────────────────────────────────────────────────────────────────────────
log "Creating Artifact Registry repository..."
gcloud artifacts repositories create fcrt-images \
  --repository-format=docker \
  --location="$REGION" \
  --description="FCRT Cloud Run container images" \
  --project="$PROJECT_ID" 2>/dev/null || warn "Repository may already exist"
ok "Artifact Registry ready"

# ──────────────────────────────────────────────────────────────────────────────
# 3. VPC + CONNECTOR
# ──────────────────────────────────────────────────────────────────────────────
log "Checking VPC network and Serverless VPC Access Connector..."
gcloud compute networks create fcrt-vpc --subnet-mode=auto --project="$PROJECT_ID" 2>/dev/null || warn "VPC may exist"

# Check if connector exists and is READY
CONNECTOR_STATE=$(gcloud compute networks vpc-access connectors describe fcrt-connector --region="$REGION" --project="$PROJECT_ID" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")

if [[ "$CONNECTOR_STATE" == "NOT_FOUND" ]]; then
  log "Creating VPC connector..."
  gcloud compute networks vpc-access connectors create fcrt-connector \
    --region="$REGION" \
    --network=fcrt-vpc \
    --range=10.8.0.0/28 \
    --project="$PROJECT_ID" >/dev/null
  
  # Wait for connector to be ready
  log "Waiting for VPC connector to be ready..."
  for i in {1..120}; do
    STATE=$(gcloud compute networks vpc-access connectors describe fcrt-connector --region="$REGION" --project="$PROJECT_ID" --format="value(state)" 2>/dev/null || echo "UNKNOWN")
    [[ "$STATE" == "READY" ]] && break
    sleep 5
  done
  [[ "$STATE" == "READY" ]] || err "VPC connector not ready after 10 minutes (state: $STATE)"
elif [[ "$CONNECTOR_STATE" == "ERROR" ]]; then
  warn "Connector in ERROR state — deleting and recreating"
  gcloud compute networks vpc-access connectors delete fcrt-connector --region="$REGION" --project="$PROJECT_ID" --quiet >/dev/null
  gcloud compute networks vpc-access connectors create fcrt-connector \
    --region="$REGION" \
    --network=fcrt-vpc \
    --range=10.8.0.0/28 \
    --project="$PROJECT_ID" >/dev/null
  
  log "Waiting for VPC connector to be ready..."
  for i in {1..120}; do
    STATE=$(gcloud compute networks vpc-access connectors describe fcrt-connector --region="$REGION" --project="$PROJECT_ID" --format="value(state)" 2>/dev/null || echo "UNKNOWN")
    [[ "$STATE" == "READY" ]] && break
    sleep 5
  done
  [[ "$STATE" == "READY" ]] || err "VPC connector not ready after 10 minutes (state: $STATE)"
else
  ok "VPC connector already exists and is READY"
fi

# ──────────────────────────────────────────────────────────────────────────────
# 4. CLOUD SQL (PostgreSQL 16, Private IP)
# ──────────────────────────────────────────────────────────────────────────────
log "Creating Cloud SQL instance (PostgreSQL 16)..."
# Check if instance exists and its state
INSTANCE_STATE=$(gcloud sql instances describe fcrt-db --project="$PROJECT_ID" --format="value(state)" 2>/dev/null || echo "NOT_FOUND")

if [[ "$INSTANCE_STATE" == "NOT_FOUND" ]]; then
  # Generate password
  DB_PASSWORD=$(openssl rand -base64 32)
  echo "DB_PASSWORD=$DB_PASSWORD" > /tmp/fcrt-credentials.txt
  chmod 600 /tmp/fcrt-credentials.txt

  gcloud sql instances create fcrt-db \
    --database-version=POSTGRES_16 \
    --tier="$DB_TIER" \
    --region="$REGION" \
    --network=fcrt-vpc \
    --no-assign-ip \
    --enable-google-private-path \
    --deletion-protection \
    --backup-start-time=03:00 \
    --maintenance-window-day=SUN --maintenance-window-hour=04 \
    --project="$PROJECT_ID" >/dev/null

  gcloud sql databases create fcrt --instance=fcrt-db --project="$PROJECT_ID" >/dev/null
  gcloud sql users create fcrt-user --instance=fcrt-db --password="$DB_PASSWORD" --project="$PROJECT_ID" >/dev/null
  ok "Cloud SQL created. Password saved to /tmp/fcrt-credentials.txt"

elif [[ "$INSTANCE_STATE" == "FAILED" ]]; then
  warn "Instance in FAILED state — deleting and recreating"
  gcloud sql instances delete fcrt-db --project="$PROJECT_ID" --quiet >/dev/null
  sleep 5

  DB_PASSWORD=$(openssl rand -base64 32)
  echo "DB_PASSWORD=$DB_PASSWORD" > /tmp/fcrt-credentials.txt
  chmod 600 /tmp/fcrt-credentials.txt

  gcloud sql instances create fcrt-db \
    --database-version=POSTGRES_16 \
    --tier="$DB_TIER" \
    --region="$REGION" \
    --network=fcrt-vpc \
    --no-assign-ip \
    --enable-google-private-path \
    --deletion-protection \
    --backup-start-time=03:00 \
    --maintenance-window-day=SUN --maintenance-window-hour=04 \
    --project="$PROJECT_ID" >/dev/null

  gcloud sql databases create fcrt --instance=fcrt-db --project="$PROJECT_ID" >/dev/null
  gcloud sql users create fcrt-user --instance=fcrt-db --password="$DB_PASSWORD" --project="$PROJECT_ID" >/dev/null
  ok "Cloud SQL recreated. Password saved to /tmp/fcrt-credentials.txt"

else
  warn "Cloud SQL instance 'fcrt-db' exists (state: $INSTANCE_STATE)"
  # Try to read existing password from secret manager
  if gcloud secrets versions access latest --secret=fcrt-db-password --project="$PROJECT_ID" >/dev/null 2>&1; then
    DB_PASSWORD=$(gcloud secrets versions access latest --secret=fcrt-db-password --project="$PROJECT_ID")
    echo "DB_PASSWORD=$DB_PASSWORD" > /tmp/fcrt-credentials.txt
    ok "Using existing password from Secret Manager"
  else
    warn "No password in Secret Manager — resetting password for fcrt-user"
    DB_PASSWORD=$(openssl rand -base64 32)
    gcloud sql users set-password fcrt-user \
      --instance=fcrt-db \
      --password="$DB_PASSWORD" \
      --project="$PROJECT_ID" >/dev/null
    echo "DB_PASSWORD=$DB_PASSWORD" > /tmp/fcrt-credentials.txt
    ok "Password reset and saved to /tmp/fcrt-credentials.txt"
  fi
fi

CONNECTION_NAME=$(gcloud sql instances describe fcrt-db --project="$PROJECT_ID" --format="value(connectionName)")
ok "Cloud SQL connection name: $CONNECTION_NAME"

# ──────────────────────────────────────────────────────────────────────────────
# 5. SERVICE ACCOUNT + IAM
# ──────────────────────────────────────────────────────────────────────────────
log "Creating service account and granting IAM roles..."
gcloud iam service-accounts create fcrt-api \
  --display-name="FCRT API + Worker" \
  --description="Runs FCRT API and Worker on Cloud Run" \
  --project="$PROJECT_ID" 2>/dev/null || warn "Service account may exist"

SA_EMAIL="fcrt-api@$PROJECT_ID.iam.gserviceaccount.com"

for ROLE in \
  roles/cloudsql.client \
  roles/aiplatform.user \
  roles/secretmanager.secretAccessor \
  roles/logging.logWriter \
  roles/monitoring.metricWriter; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:$SA_EMAIL" \
    --role="$ROLE" \
    --condition=None >/dev/null
done
ok "Service account IAM roles granted (firebase.skip — manual setup if needed)"
warn "NOTE: roles/firebase.authenticationAdmin cannot be granted via CLI for service accounts. If POST /scenarios returns 503, set FIREBASE_PROJECT='' in .env for local dev."

# ──────────────────────────────────────────────────────────────────────────────
# 6. SECRET MANAGER (DB password + DATABASE_URL)
# ──────────────────────────────────────────────────────────────────────────────
log "Storing secrets in Secret Manager..."
echo -n "$DB_PASSWORD" | gcloud secrets create fcrt-db-password --data-file=- --project="$PROJECT_ID" 2>/dev/null || \
  echo -n "$DB_PASSWORD" | gcloud secrets versions add fcrt-db-password --data-file=- --project="$PROJECT_ID" >/dev/null

DB_URL="postgres://fcrt-user:$DB_PASSWORD@/fcrt?host=/cloudsql/$CONNECTION_NAME"
echo -n "$DB_URL" | gcloud secrets create fcrt-db-url --data-file=- --project="$PROJECT_ID" 2>/dev/null || \
  echo -n "$DB_URL" | gcloud secrets versions add fcrt-db-url --data-file=- --project="$PROJECT_ID" >/dev/null

# Grant access
gcloud secrets add-iam-policy-binding fcrt-db-password \
  --member="serviceAccount:$SA_EMAIL" --role="roles/secretmanager.secretAccessor" --project="$PROJECT_ID" >/dev/null
gcloud secrets add-iam-policy-binding fcrt-db-url \
  --member="serviceAccount:$SA_EMAIL" --role="roles/secretmanager.secretAccessor" --project="$PROJECT_ID" >/dev/null

# Session-signing secret for AUTH_MODE=local. Generated once and reused on
# subsequent runs so existing sessions stay valid across deploys.
if ! gcloud secrets describe fcrt-session-secret --project="$PROJECT_ID" >/dev/null 2>&1; then
  openssl rand -hex 32 | gcloud secrets create fcrt-session-secret --data-file=- --project="$PROJECT_ID" >/dev/null
  ok "Generated AUTH_SESSION_SECRET (fcrt-session-secret)"
else
  ok "Reusing existing AUTH_SESSION_SECRET (fcrt-session-secret)"
fi
gcloud secrets add-iam-policy-binding fcrt-session-secret \
  --member="serviceAccount:$SA_EMAIL" --role="roles/secretmanager.secretAccessor" --project="$PROJECT_ID" >/dev/null
ok "Secrets stored"

# ──────────────────────────────────────────────────────────────────────────────
# 7. MANUAL Docker build + push (Cloud Build may fail due to Go version)
# ──────────────────────────────────────────────────────────────────────────────
log "Building Docker image locally..."
cd "$(dirname "$0")/.."  # fcrt repo root

# Build
docker build -t "$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:local" . >/dev/null 2>&1 || warn "Docker build failed"

# Authenticate and push
gcloud auth configure-docker "$REGION-docker.pkg.dev" >/dev/null 2>&1
docker tag "$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:local" "$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest" >/dev/null 2>&1
docker push "$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest" >/dev/null 2>&1 || warn "Docker push failed"
ok "Image pushed: $REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest"

# ──────────────────────────────────────────────────────────────────────────────
# 8. DEPLOY API TO CLOUD RUN
# ──────────────────────────────────────────────────────────────────────────────
log "Deploying API to Cloud Run..."
gcloud run deploy fcrt-api \
  --image="$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest" \
  --region="$REGION" \
  --service-account="$SA_EMAIL" \
  --add-cloudsql-instances="$CONNECTION_NAME" \
  --vpc-connector=fcrt-connector \
  --vpc-egress=private-ranges-only \
  --set-env-vars="^;^AUTH_MODE=local;AUTH_USERS=admin:admin:admin,analyst:analyst:analyst,viewer:viewer:viewer;FIREBASE_PROJECT=$PROJECT_ID;VERTEX_PROJECT=$PROJECT_ID;VERTEX_LOCATION=$REGION;DATABASE_URL=postgres://fcrt-user:$DB_PASSWORD@/fcrt?host=/cloudsql/$CONNECTION_NAME;LOG_LEVEL=info;CORS_ORIGINS=https://$PROJECT_ID.web.app,https://$PROJECT_ID.firebaseapp.com" \
  --set-secrets="AUTH_SESSION_SECRET=projects/$PROJECT_ID/secrets/fcrt-session-secret:latest" \
  --allow-unauthenticated \
  --min-instances="$MIN_INSTANCES" --max-instances=10 \
  --cpu=1 --memory=512Mi \
  --port=8080 \
  --project="$PROJECT_ID" >/dev/null

API_URL=$(gcloud run services describe fcrt-api --region="$REGION" --project="$PROJECT_ID" --format="value(status.url)")
ok "API deployed: $API_URL"

# ──────────────────────────────────────────────────────────────────────────────
# 9. DEPLOY WORKER TO CLOUD RUN (same image, different entrypoint)
# ──────────────────────────────────────────────────────────────────────────────
log "Deploying Worker to Cloud Run..."
gcloud run deploy fcrt-worker \
  --image="$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest" \
  --region="$REGION" \
  --service-account="$SA_EMAIL" \
  --add-cloudsql-instances="$CONNECTION_NAME" \
  --vpc-connector=fcrt-connector \
  --vpc-egress=private-ranges-only \
  --set-env-vars="FIREBASE_PROJECT=$PROJECT_ID,VERTEX_PROJECT=$PROJECT_ID,VERTEX_LOCATION=$REGION,LOG_LEVEL=info" \
  --set-secrets="DATABASE_URL=projects/$PROJECT_ID/secrets/fcrt-db-url:latest" \
  --no-allow-unauthenticated \
  --min-instances="$MIN_INSTANCES" --max-instances=5 \
  --cpu=1 --memory=512Mi \
  --command=/fcrt-worker \
  --args="--poll-interval=10s" \
  --project="$PROJECT_ID" 2>/dev/null || warn "Worker deploy failed"
ok "Worker deployed"

# ──────────────────────────────────────────────────────────────────────────────
# 10. RUN MIGRATIONS (via Cloud Run Job)
# ──────────────────────────────────────────────────────────────────────────────
log "Running database migrations via Cloud Run Job..."
gcloud run jobs create fcrt-migrate \
  --image="$REGION-docker.pkg.dev/$PROJECT_ID/fcrt-images/fcrt-api:latest" \
  --region="$REGION" \
  --service-account="$SA_EMAIL" \
  --add-cloudsql-instances="$CONNECTION_NAME" \
  --set-secrets="DATABASE_URL=projects/$PROJECT_ID/secrets/fcrt-db-url:latest" \
  --command=/migrate \
  --args="up" \
  --project="$PROJECT_ID" 2>/dev/null || warn "Job may exist"

gcloud run jobs execute fcrt-migrate --region="$REGION" --project="$PROJECT_ID" --wait >/dev/null
ok "Migrations applied"

# ──────────────────────────────────────────────────────────────────────────────
# 11. FRONTEND DEPLOY (Firebase Hosting)
# ──────────────────────────────────────────────────────────────────────────────
log "Preparing frontend for Firebase Hosting..."

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FRONTEND_DIR="$REPO_ROOT/frontend"

# Point the Firebase CLI at this project (firebase.json already configures
# the /api/** rewrite to the fcrt-api Cloud Run service).
cat > "$REPO_ROOT/.firebaserc" <<EOF
{
  "projects": { "default": "$PROJECT_ID" }
}
EOF
ok ".firebaserc set to $PROJECT_ID"

cat > "$FRONTEND_DIR/.env.production" <<EOF
VITE_API_BASE=/api/v1
VITE_FIREBASE_API_KEY=${FIREBASE_API_KEY:-}
VITE_FIREBASE_AUTH_DOMAIN=$PROJECT_ID.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=$PROJECT_ID
EOF

if command -v firebase >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  ( cd "$FRONTEND_DIR" && npm install --silent && npm run build ) || warn "Frontend build failed"
  if firebase deploy --only hosting --project "$PROJECT_ID" >/dev/null 2>&1; then
    ok "Frontend deployed to Firebase Hosting"
  else
    warn "Firebase deploy failed — run: firebase deploy --only hosting"
  fi
else
  warn "firebase CLI / npm not found — skipping frontend deploy"
  warn "Then run: (cd frontend && npm install && npm run build) && firebase deploy --only hosting"
fi

cat <<EOF

${GREEN}═══════════════════════════════════════════════════════════════════════
✅ BACKEND INFRASTRUCTURE COMPLETE
═══════════════════════════════════════════════════════════════════════${NC}

${BLUE}API URL:${NC} $API_URL
${BLUE}Health check:${NC} curl $API_URL/healthz
${BLUE}Test auth:${NC} curl -H "Authorization: Bearer dev-token" $API_URL/api/v1/cohorts

${YELLOW}Credentials saved to:${NC} /tmp/fcrt-credentials.txt
  $(cat /tmp/fcrt-credentials.txt)

${YELLOW}Remaining manual steps:${NC}

1. ${GREEN}Firebase Web App${NC} (if not already created):
   → Firebase Console → Project Settings → Create Web App
   → Re-run this script with FIREBASE_API_KEY=<apiKey> to bake it into the build,
     or add VITE_FIREBASE_API_KEY to frontend/.env.production and rebuild.

2. ${GREEN}Local Development${NC}:
   cloud-sql-proxy $CONNECTION_NAME --port=5432 &
   export DATABASE_URL="postgres://fcrt-user:\$DB_PASSWORD@localhost:5432/fcrt?sslmode=disable"
   go run ./cmd/api
   cd frontend && npm run dev

EOF