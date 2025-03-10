#!/bin/bash

repo="vmturbo/logging-agent"
tag="redhat-6.1dev"
docker build -t $repo:$tag .
