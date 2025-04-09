#!/bin/bash
set -e

# Create a temporary build directory
mkdir -p build

# Get dependencies
go mod tidy

# Build the Lambda function for Amazon Linux 2
GOOS=linux GOARCH=amd64 go build -o build/main main.go

# Move to the build directory
cd build

# Copy the bootstrap file
cp ../bootstrap .

# Make sure bootstrap is executable (set execute permission explicitly)
chmod 755 bootstrap

# Zip the binary and bootstrap for Lambda deployment
zip main.zip main bootstrap

# Print file permissions as a sanity check
echo "File permissions in zip:"
unzip -v main.zip

# Move the zip file to the parent directory
mv main.zip ..

# Clean up
cd ..
rm -rf build

echo "Lambda function built successfully" 