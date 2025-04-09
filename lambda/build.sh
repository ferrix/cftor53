#!/bin/bash
set -e

# Create a temporary build directory
mkdir -p build

# Get dependencies
go mod tidy

# Build the Go binary for AWS Lambda (Amazon Linux 2 x86_64)
echo "Building Lambda function..."
GOOS=linux GOARCH=amd64 go build -o build/main main.go

# Move to the build directory
cd build

# Create the bootstrap file
cat > bootstrap << 'EOF'
#!/bin/sh
./main
EOF

# Make the bootstrap file executable
chmod +x bootstrap

# Create the Lambda deployment package
echo "Creating Lambda deployment package (main.zip)..."
zip main.zip main bootstrap

# Verify file permissions in the zip
echo "File permissions in zip:"
unzip -l main.zip

# Move the zip file to the parent directory
mv main.zip ..

# Clean up
cd ..
rm -rf build

# Display success message
echo "Lambda function built successfully"

# Instructions for testing
echo ""
echo "To run tests: cd .. && go test -v"
echo "To run static analysis: cd .. && go install honnef.co/go/tools/cmd/staticcheck@latest && staticcheck ./..." 