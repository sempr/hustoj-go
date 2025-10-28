#!/bin/bash

echo $1 $2

V=$(docker inspect $1 -f "base = \"{{.GraphDriver.Data.UpperDir}}:{{.GraphDriver.Data.LowerDir}}\"")
echo $V
sed -i.bak "s|^base = .*|${V}|g" $2
