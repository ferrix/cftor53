# cftor53

A CDK-powered tool for delegating Cloudflare subdomains to AWS Route53 with automatic ACM certificate provisioning.

## Overview

cftor53 automates the process of setting up a subdomain delegation from a Cloudflare-managed domain to AWS Route53, with ACM certificate validation handled automatically. The Lambda function checks for conflicting DNS records and manages the NS record updates in Cloudflare.

## Features

- Create a Route53 hosted zone for your subdomain
- Delegate subdomain DNS management from Cloudflare to Route53
- Automatically provision ACM certificates in us-east-1 (for CloudFront)
- Cross-region support (main resources and certificates can be in different regions)
- Handle DNS validation automatically

## Prerequisites

- Go 1.18+
- AWS CDK v2
- An AWS account with appropriate permissions
- A domain managed in Cloudflare
- Cloudflare API token with Zone:Read and DNS:Edit permissions

## Configuration

Configuration is managed through a `config.json` file in the project root. Here's a sample configuration:

```json
{
  "api_token": "your-cloudflare-api-token",
  "parent_domain": "example.com",
  "subdomain": "api",
  "regions": {
    "main": "eu-north-1",
    "certificate": "us-east-1"
  },
  "secret_name": "cftor53/cloudflare/api-token",
  "ssm_param_prefix": "/cftor53",
  "lambda_settings": {
    "timeout_seconds": 120,
    "memory_size_mb": 256
  }
}
```

### Configuration Fields

| Field | Description | Required | Default |
|-------|-------------|----------|---------|
| `api_token` | Cloudflare API token | Yes | N/A |
| `parent_domain` | Your domain managed in Cloudflare | Yes | N/A |
| `subdomain` | The subdomain to delegate to Route53 | Yes | N/A |
| `regions.main` | AWS region for main resources | No | eu-north-1 |
| `regions.certificate` | AWS region for certificates | No | us-east-1 |
| `secret_name` | AWS Secrets Manager name for the token | No | cftor53/cloudflare/api-token |
| `ssm_param_prefix` | Prefix for SSM parameters | No | /cftor53 |
| `lambda_settings.timeout_seconds` | Lambda timeout | No | 120 |
| `lambda_settings.memory_size_mb` | Lambda memory | No | 256 |

## Deployment

### Building the Lambda function

```bash
cd lambda
./build.sh
```

### Deploying with CDK

```bash
# Make sure CDK is bootstrapped in your account
npx cdk bootstrap

# Deploy all stacks
npx cdk deploy --all
```

## How It Works

1. Secrets Stack (`CfCloudflareSecretsStack`): Stores your Cloudflare API token securely in AWS Secrets Manager.

2. Main Stack (`Cftor53Stack`): 
   - Creates a Lambda function to interact with Cloudflare API
   - Checks for conflicting DNS records in Cloudflare
   - Creates a Route53 hosted zone for your subdomain
   - Updates Cloudflare NS records to point to Route53 name servers

3. Certificate Stack (`Cftor53CertificateStack`):
   - Creates an ACM certificate in us-east-1 region (required for CloudFront)
   - Uses DNS validation with the Route53 hosted zone
   - Stores the certificate ARN in SSM Parameter Store for reference

## Error Handling

The Lambda function has two phases:

1. **DNS Check Phase**: Fails if any conflicting (non-NS) records exist for the subdomain in Cloudflare.

2. **NS Update Phase**: Updates NS records to point to Route53 name servers. 
   - A partial failure during NS record additions/deletions is logged but does not abort the deployment
   - The deployment succeeds as long as at least one NS record is successfully added

## Troubleshooting

### Invalid Access Token

If you see `Invalid access token` errors, check that:
- Your Cloudflare API token is correct and has the required permissions
- The token is properly stored in Secrets Manager

### Cross-Region Deployment Issues

For cross-region deployment errors, ensure:
- CDK is bootstrapped in all regions you're using
- Cross-region references are enabled in your CDK app

## License

This project is licensed under the MIT License - see the LICENSE file for details.
