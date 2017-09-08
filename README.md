rancher-net
========

This repository is used for packaging all the networking related
binaries into a single image.

You can find the actual code here:
- IPSec: https://github.com/rancher/ipsec
- VXLAN: https://github.com/rancher/vxlan
- Rancher CNI Bridge Plugin: https://github.com/rancher/rancher-cni-bridge
- Rancher CNI IPAM Plugin: https://github.com/rancher/rancher-cni-ipam

## Building

`make`

If you would like to build using a custom repo and tag:

`REPO=your_docker_repo TAG=dev_or_sth_else make release`

The individual binaries can be customized during the build using the following:

Repos:

- RANCHER_IPSEC_REPO
- RANCHER_VXLAN_REPO
- RANCHER_CNI_BRIDGE_REPO
- RANCHER_CNI_IPAM_REPO

Tags:

- RANCHER_IPSEC_TAG
- RANCHER_VXLAN_TAG
- RANCHER_CNI_BRIDGE_TAG
- RANCHER_CNI_IPAM_TAG


For example:

```
REPO=leodotcloud TAG=test RANCHER_IPSEC_REPO=leodotcloud RANCHER_IPSEC_TAG=dev make
```

This would pick up the ipsec binaries from the private `dev` release and package them in the docker image `leodotcloud/net:test`.

## Running

Different Networking catalog items are built using `rancher/net` docker image and one of them can be selected here: https://github.com/rancher/rancher-catalog

## License
Copyright (c) 2014-2017 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
