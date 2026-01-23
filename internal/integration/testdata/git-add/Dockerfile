# Test Dockerfile with git source in ADD instruction
# BuildKit supports fetching git repositories via ADD
FROM alpine:3.18

# Add a git repository - BuildKit will clone this during build
# Using cli/cli (GitHub CLI) with a stable release tag
ADD https://github.com/cli/cli.git#v2.40.0 /cli-src

RUN ls -la /cli-src
