# NovaCoin AWS Deployment

Automated deployment of NovaCoin testnet/mainnet infrastructure on AWS.

## Prerequisites

1. **Terraform** >= 1.5.0
   ```bash
   # macOS
   brew install terraform

   # Linux
   wget https://releases.hashicorp.com/terraform/1.6.0/terraform_1.6.0_linux_amd64.zip
   unzip terraform_1.6.0_linux_amd64.zip
   sudo mv terraform /usr/local/bin/
   ```

2. **AWS CLI** >= 2.0
   ```bash
   # macOS
   brew install awscli

   # Linux
   curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
   unzip awscliv2.zip
   sudo ./aws/install
   ```

3. **jq**
   ```bash
   # macOS
   brew install jq

   # Linux
   sudo apt-get install jq
   ```

## Quick Start

### 1. Create AWS IAM User & Get Access Keys

If you don't have AWS access keys yet, follow these steps:

#### Step 1: Go to IAM Console
Open: https://console.aws.amazon.com/iam/home#/users

#### Step 2: Create User
1. Click **"Create user"** (orange button)
2. Enter User name: `novacoin-deploy`
3. **DO NOT check** "Provide user access to the AWS Management Console"
4. Click **Next**

#### Step 3: Set Permissions
1. Select **"Attach policies directly"**
2. Search and check these policies:
   - `AmazonEC2FullAccess`
   - `ElasticLoadBalancingFullAccess`
   - `IAMFullAccess`

   Or for testing, just check: `AdministratorAccess`
3. Click **Next** → **Create user**

#### Step 4: Create Access Key
1. Click on the user name **"novacoin-deploy"**
2. Go to **"Security credentials"** tab
3. Scroll to **"Access keys"** section
4. Click **"Create access key"**
5. Select **"Command Line Interface (CLI)"**
6. Check the confirmation checkbox
7. Click **Next** → **Create access key**
8. **IMPORTANT:** Copy both keys now (secret key shown only once!)

```
Access key ID:     AKIAXXXXXXXXXXXXXXXX
Secret access key: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 2. Configure AWS Credentials

```bash
./deploy.sh setup
```

Enter your Access Key ID and Secret Access Key when prompted.

Or set environment variables:
```bash
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="ap-southeast-1"
```

### 3. Review Configuration (Optional)

Edit `terraform.tfvars` to customize:
```bash
cp terraform.tfvars.example terraform.tfvars
vim terraform.tfvars
```

### 4. Preview Deployment

```bash
./deploy.sh plan
```

### 5. Deploy Infrastructure

```bash
./deploy.sh apply
```

### 6. Check Status

```bash
./deploy.sh status
```

## Infrastructure

Default deployment (light testnet config):

| Component | Count | Instance Type | Purpose |
|-----------|-------|---------------|---------|
| Bootnode | 1 | t3.small | Network bootstrap & discovery |
| Validators | 4 | t3.medium | Block production & consensus |
| RPC Nodes | 1 | t3.small | Public API access |
| Load Balancer | 1 | ALB | RPC traffic distribution |

**Region:** ap-southeast-1 (Singapore)
**Minimum validators:** 4 (required for BFT 2/3 quorum)

## Network Diagram

```
                    Internet
                        │
                        ▼
               ┌───────────────┐
               │  Application  │
               │ Load Balancer │
               └───────┬───────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
        ▼              ▼              ▼
   ┌─────────┐   ┌─────────┐   ┌─────────┐
   │ RPC-1   │   │ RPC-2   │   │ RPC-N   │
   └────┬────┘   └────┬────┘   └────┬────┘
        │              │              │
        └──────────────┼──────────────┘
                       │
               ┌───────┴───────┐
               │   Bootnode    │
               └───────┬───────┘
                       │
   ┌───────────────────┼───────────────────┐
   │                   │                   │
   ▼                   ▼                   ▼
┌──────────┐   ┌──────────┐         ┌──────────┐
│Validator1│   │Validator2│  ...    │ValidatorN│
└──────────┘   └──────────┘         └──────────┘
```

## Commands

| Command | Description |
|---------|-------------|
| `./deploy.sh setup` | Configure AWS credentials |
| `./deploy.sh plan` | Preview infrastructure changes |
| `./deploy.sh apply` | Deploy infrastructure |
| `./deploy.sh destroy` | Tear down infrastructure |
| `./deploy.sh status` | Check deployment status |
| `./deploy.sh ssh bootnode` | SSH into bootnode |
| `./deploy.sh ssh validator-1` | SSH into validator 1 |
| `./deploy.sh ssh rpc-1` | SSH into RPC node 1 |
| `./deploy.sh logs validator-1` | Stream validator 1 logs |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ENVIRONMENT` | testnet | Network environment |
| `AWS_REGION` | us-east-1 | AWS region |
| `VALIDATOR_COUNT` | 4 | Number of validators |
| `RPC_NODE_COUNT` | 2 | Number of RPC nodes |

## Outputs

After deployment, you'll receive:

- **Bootnode IP** - For P2P network bootstrap
- **Validator IPs** - For monitoring
- **RPC Endpoint** - Public API: `http://<lb-dns>:8545`
- **SSH Key** - In `keys/novacoin-<env>.pem`

## Costs (Estimated)

| Environment | Monthly Cost (USD) |
|-------------|-------------------|
| Light Testnet (4 validators, 1 RPC) | ~$150 |
| Production Testnet (4 validators, 2 RPC) | ~$200 |
| Mainnet (10 validators, 4 RPC) | ~$500+ |

**Light testnet breakdown:**
- 1x Bootnode (t3.small): ~$15/mo
- 4x Validators (t3.medium): ~$120/mo
- 1x RPC Node (t3.small): ~$15/mo

## Security

- SSH access restricted to specified CIDRs
- Validator RPC only accessible internally
- All EBS volumes encrypted
- Nodes run as non-root user
- Firewall (UFW) enabled on all nodes

## Troubleshooting

### Check node status
```bash
./deploy.sh ssh bootnode
sudo systemctl status novacoin
sudo journalctl -u novacoin -f
```

### Check sync status
```bash
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_syncing","params":[],"id":1}'
```

### Restart node
```bash
./deploy.sh ssh validator-1
sudo systemctl restart novacoin
```

## Cleanup

To destroy all infrastructure:
```bash
./deploy.sh destroy
```

**WARNING:** This permanently deletes all nodes and data!
