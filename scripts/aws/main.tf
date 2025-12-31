# NovaCoin AWS Infrastructure
# Terraform configuration for automated testnet deployment

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5"
    }
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "NovaCoin"
      Environment = var.environment
      ManagedBy   = "Terraform"
    }
  }
}

# =============================================================================
# VARIABLES
# =============================================================================

variable "aws_region" {
  description = "AWS region for deployment"
  type        = string
  default     = "ap-southeast-1"  # Singapore
}

variable "environment" {
  description = "Environment name (testnet/mainnet)"
  type        = string
  default     = "testnet"
}

variable "chain_id" {
  description = "Blockchain chain ID"
  type        = number
  default     = 2
}

variable "validator_count" {
  description = "Number of validators to deploy"
  type        = number
  default     = 4  # Minimum required for BFT consensus (2/3 quorum)
}

variable "rpc_node_count" {
  description = "Number of RPC nodes to deploy"
  type        = number
  default     = 1  # Single RPC for testing
}

variable "ssh_allowed_cidrs" {
  description = "CIDR blocks allowed for SSH access"
  type        = list(string)
  default     = ["0.0.0.0/0"]  # Restrict this in production!
}

variable "instance_type_bootnode" {
  description = "EC2 instance type for bootnode"
  type        = string
  default     = "t3.small"  # 2 vCPU, 2GB RAM - $0.02/hr
}

variable "instance_type_validator" {
  description = "EC2 instance type for validators"
  type        = string
  default     = "t3.medium"  # 2 vCPU, 4GB RAM - $0.04/hr
}

variable "instance_type_rpc" {
  description = "EC2 instance type for RPC nodes"
  type        = string
  default     = "t3.small"  # 2 vCPU, 2GB RAM - $0.02/hr
}

variable "volume_size_bootnode" {
  description = "EBS volume size for bootnode (GB)"
  type        = number
  default     = 30
}

variable "volume_size_validator" {
  description = "EBS volume size for validators (GB)"
  type        = number
  default     = 50
}

variable "volume_size_rpc" {
  description = "EBS volume size for RPC nodes (GB)"
  type        = number
  default     = 50
}

# =============================================================================
# DATA SOURCES
# =============================================================================

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]  # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# =============================================================================
# NETWORKING
# =============================================================================

resource "aws_vpc" "novacoin" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "novacoin-${var.environment}-vpc"
  }
}

resource "aws_internet_gateway" "novacoin" {
  vpc_id = aws_vpc.novacoin.id

  tags = {
    Name = "novacoin-${var.environment}-igw"
  }
}

resource "aws_subnet" "public" {
  count                   = 3
  vpc_id                  = aws_vpc.novacoin.id
  cidr_block              = "10.0.${count.index + 1}.0/24"
  availability_zone       = data.aws_availability_zones.available.names[count.index % length(data.aws_availability_zones.available.names)]
  map_public_ip_on_launch = true

  tags = {
    Name = "novacoin-${var.environment}-public-${count.index + 1}"
    Type = "public"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.novacoin.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.novacoin.id
  }

  tags = {
    Name = "novacoin-${var.environment}-public-rt"
  }
}

resource "aws_route_table_association" "public" {
  count          = length(aws_subnet.public)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# =============================================================================
# SECURITY GROUPS
# =============================================================================

resource "aws_security_group" "bootnode" {
  name        = "novacoin-${var.environment}-bootnode-sg"
  description = "Security group for NovaCoin bootnode"
  vpc_id      = aws_vpc.novacoin.id

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidrs
    description = "SSH access"
  }

  # P2P TCP
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "P2P TCP"
  }

  # P2P UDP
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "P2P UDP"
  }

  # RPC HTTP
  ingress {
    from_port   = 8545
    to_port     = 8545
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RPC HTTP"
  }

  # RPC WebSocket
  ingress {
    from_port   = 8546
    to_port     = 8546
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "RPC WebSocket"
  }

  # Metrics (internal)
  ingress {
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "Prometheus metrics"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name = "novacoin-${var.environment}-bootnode-sg"
  }
}

resource "aws_security_group" "validator" {
  name        = "novacoin-${var.environment}-validator-sg"
  description = "Security group for NovaCoin validators"
  vpc_id      = aws_vpc.novacoin.id

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidrs
    description = "SSH access"
  }

  # P2P TCP
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "P2P TCP"
  }

  # P2P UDP
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "P2P UDP"
  }

  # Metrics (internal + monitoring)
  ingress {
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "Prometheus metrics"
  }

  # RPC (internal only for validators)
  ingress {
    from_port   = 8545
    to_port     = 8546
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "Internal RPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name = "novacoin-${var.environment}-validator-sg"
  }
}

resource "aws_security_group" "rpc" {
  name        = "novacoin-${var.environment}-rpc-sg"
  description = "Security group for NovaCoin RPC nodes"
  vpc_id      = aws_vpc.novacoin.id

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidrs
    description = "SSH access"
  }

  # P2P
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 30303
    to_port     = 30303
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # RPC HTTP (public)
  ingress {
    from_port   = 8545
    to_port     = 8545
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Public RPC HTTP"
  }

  # RPC WebSocket (public)
  ingress {
    from_port   = 8546
    to_port     = 8546
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Public RPC WebSocket"
  }

  # Metrics
  ingress {
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/16"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "novacoin-${var.environment}-rpc-sg"
  }
}

# =============================================================================
# SSH KEY
# =============================================================================

resource "tls_private_key" "novacoin" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "novacoin" {
  key_name   = "novacoin-${var.environment}-key"
  public_key = tls_private_key.novacoin.public_key_openssh

  tags = {
    Name = "novacoin-${var.environment}-key"
  }
}

resource "local_file" "private_key" {
  content         = tls_private_key.novacoin.private_key_pem
  filename        = "${path.module}/keys/novacoin-${var.environment}.pem"
  file_permission = "0600"
}

# =============================================================================
# IAM ROLE FOR NODES
# =============================================================================

resource "aws_iam_role" "novacoin_node" {
  name = "novacoin-${var.environment}-node-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "novacoin_node" {
  name = "novacoin-${var.environment}-node-policy"
  role = aws_iam_role.novacoin_node.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ec2:DescribeInstances",
          "ec2:DescribeTags"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_instance_profile" "novacoin_node" {
  name = "novacoin-${var.environment}-node-profile"
  role = aws_iam_role.novacoin_node.name
}

# =============================================================================
# EC2 INSTANCES - BOOTNODE
# =============================================================================

resource "aws_instance" "bootnode" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type_bootnode
  subnet_id              = aws_subnet.public[0].id
  vpc_security_group_ids = [aws_security_group.bootnode.id]
  key_name               = aws_key_pair.novacoin.key_name
  iam_instance_profile   = aws_iam_instance_profile.novacoin_node.name

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.volume_size_bootnode
    iops                  = 3000   # Default for gp3
    throughput            = 125    # Default for gp3
    delete_on_termination = true
    encrypted             = true
  }

  user_data = base64encode(templatefile("${path.module}/scripts/bootnode-init.sh", {
    chain_id    = var.chain_id
    environment = var.environment
  }))

  tags = {
    Name = "novacoin-${var.environment}-bootnode"
    Role = "bootnode"
  }
}

# =============================================================================
# EC2 INSTANCES - VALIDATORS
# =============================================================================

resource "aws_instance" "validator" {
  count                  = var.validator_count
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type_validator
  subnet_id              = aws_subnet.public[count.index % length(aws_subnet.public)].id
  vpc_security_group_ids = [aws_security_group.validator.id]
  key_name               = aws_key_pair.novacoin.key_name
  iam_instance_profile   = aws_iam_instance_profile.novacoin_node.name

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.volume_size_validator
    iops                  = 3000   # Default for gp3
    throughput            = 125    # Default for gp3
    delete_on_termination = true
    encrypted             = true
  }

  user_data = base64encode(templatefile("${path.module}/scripts/validator-init.sh", {
    chain_id         = var.chain_id
    environment      = var.environment
    validator_index  = count.index + 1
    bootnode_ip      = aws_instance.bootnode.private_ip
  }))

  depends_on = [aws_instance.bootnode]

  tags = {
    Name           = "novacoin-${var.environment}-validator-${count.index + 1}"
    Role           = "validator"
    ValidatorIndex = count.index + 1
  }
}

# =============================================================================
# EC2 INSTANCES - RPC NODES
# =============================================================================

resource "aws_instance" "rpc" {
  count                  = var.rpc_node_count
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type_rpc
  subnet_id              = aws_subnet.public[count.index % length(aws_subnet.public)].id
  vpc_security_group_ids = [aws_security_group.rpc.id]
  key_name               = aws_key_pair.novacoin.key_name
  iam_instance_profile   = aws_iam_instance_profile.novacoin_node.name

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.volume_size_rpc
    iops                  = 3000   # Default for gp3
    throughput            = 125    # Default for gp3
    delete_on_termination = true
    encrypted             = true
  }

  user_data = base64encode(templatefile("${path.module}/scripts/rpc-init.sh", {
    chain_id    = var.chain_id
    environment = var.environment
    bootnode_ip = aws_instance.bootnode.private_ip
  }))

  depends_on = [aws_instance.bootnode]

  tags = {
    Name = "novacoin-${var.environment}-rpc-${count.index + 1}"
    Role = "rpc"
  }
}

# =============================================================================
# LOAD BALANCER FOR RPC
# =============================================================================

resource "aws_lb" "rpc" {
  name               = "novacoin-${var.environment}-rpc-lb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.rpc.id]
  subnets            = aws_subnet.public[*].id

  tags = {
    Name = "novacoin-${var.environment}-rpc-lb"
  }
}

resource "aws_lb_target_group" "rpc_http" {
  name     = "novacoin-${var.environment}-rpc-http"
  port     = 8545
  protocol = "HTTP"
  vpc_id   = aws_vpc.novacoin.id

  health_check {
    enabled             = true
    healthy_threshold   = 2
    interval            = 30
    matcher             = "200"
    path                = "/"
    port                = "8545"
    protocol            = "HTTP"
    timeout             = 5
    unhealthy_threshold = 3
  }
}

resource "aws_lb_target_group_attachment" "rpc_http" {
  count            = var.rpc_node_count
  target_group_arn = aws_lb_target_group.rpc_http.arn
  target_id        = aws_instance.rpc[count.index].id
  port             = 8545
}

resource "aws_lb_listener" "rpc_http" {
  load_balancer_arn = aws_lb.rpc.arn
  port              = 8545
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.rpc_http.arn
  }
}

# =============================================================================
# OUTPUTS
# =============================================================================

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.novacoin.id
}

output "bootnode_public_ip" {
  description = "Bootnode public IP"
  value       = aws_instance.bootnode.public_ip
}

output "bootnode_private_ip" {
  description = "Bootnode private IP"
  value       = aws_instance.bootnode.private_ip
}

output "validator_public_ips" {
  description = "Validator public IPs"
  value       = aws_instance.validator[*].public_ip
}

output "validator_private_ips" {
  description = "Validator private IPs"
  value       = aws_instance.validator[*].private_ip
}

output "rpc_public_ips" {
  description = "RPC node public IPs"
  value       = aws_instance.rpc[*].public_ip
}

output "rpc_load_balancer_dns" {
  description = "RPC Load Balancer DNS"
  value       = aws_lb.rpc.dns_name
}

output "ssh_private_key_path" {
  description = "Path to SSH private key"
  value       = local_file.private_key.filename
}

output "ssh_command_bootnode" {
  description = "SSH command for bootnode"
  value       = "ssh -i ${local_file.private_key.filename} ubuntu@${aws_instance.bootnode.public_ip}"
}

output "rpc_endpoint" {
  description = "Public RPC endpoint"
  value       = "http://${aws_lb.rpc.dns_name}:8545"
}
