#!/bin/bash

repo="vmturbo/logging-agent"
tag="dev"
docker build -t $repo:$tag .
