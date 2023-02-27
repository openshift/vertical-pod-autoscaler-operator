#!/bin/bash

# after changing this value as part of a rebase, run this script to update deps
release_branch="release-4.13"

echo Updating OpenShift deps to $release_branch

all_mods="$(go list -mod=readonly -m -f '{{ if and (not .Indirect) (not .Main)}}{{.Path}}{{end}}' all)"

for i in $(echo "$all_mods" | grep '^github.com/openshift/'); do
    echo go get $i@$release_branch
    go get $i@$release_branch
done

echo
echo Updating all deps
for m in $all_mods; do
    echo go get -u $m
    go get -u $m
done

echo
echo go mod tidy
go mod tidy

echo
echo go mod vendor
go mod vendor
