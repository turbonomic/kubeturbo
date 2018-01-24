#!/bin/bash

repo="vmturbo/logging-agent"
tag="redhat-dev"
docker build -t $repo:$tag .
