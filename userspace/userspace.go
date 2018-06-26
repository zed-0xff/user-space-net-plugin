// Copyright 2017 Intel Corp.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniSpecVersion "github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/Billy99/user-space-net-plugin/usrsptypes"
	"github.com/Billy99/user-space-net-plugin/cnivpp/cnivpp"

	"github.com/vishvananda/netlink"
)


func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

//
// Local functions
//

// loadNetConf() - Unmarshall the inputdata into the NetConf Structure 
func loadNetConf(bytes []byte) (*usrsptypes.NetConf, error) {
	n := &usrsptypes.NetConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %v", err)
	}

	return n, nil
}


func cmdAdd(args *skel.CmdArgs) error {
	var result *current.Result
	var netConf *usrsptypes.NetConf
	var containerEngine string
	var ipData usrsptypes.IPDataType
	var prefix int


	// Convert the input bytestream into local NetConf structure
	netConf, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}


	//
	// HOST:
	//

	// Add the requested interface and network
	if netConf.HostConf.Engine == "vpp" {
		err = cnivpp.CniVppAddOnHost(netConf, ipData, args.ContainerID)
		if err != nil {
			return err
		}
	} else if netConf.HostConf.Engine == "ovs-dpdk" {
		return fmt.Errorf("GOOD: Found Host Engine:" + netConf.HostConf.Engine + " - NOT SUPPORTED")
	} else {
		return fmt.Errorf("ERROR: Unknown Host Engine:" + netConf.HostConf.Engine)
	}


	//
	// CONTAINER:
	//

	// Get IPAM data for Container Interface, if provided.
	if netConf.IPAM.Type != "" {

		//type IPConfig struct {
		//	IP      net.IPNet
		//	Gateway net.IP
		//	Routes  []types.Route
		//}

		//type Result struct {
		//	CNIVersion string    `json:"cniVersion,omitempty"`
		//	IP4        *IPConfig `json:"ip4,omitempty"`
		//	IP6        *IPConfig `json:"ip6,omitempty"`
		//	DNS        types.DNS `json:"dns,omitempty"`
		//}


		// run the IPAM plugin and get back the config to apply
		ipamResult, err := ipam.ExecAdd(netConf.IPAM.Type, args.StdinData)
		if err != nil {
			return err
		}

		// Convert whatever the IPAM result was into the current Result type
		result, err = current.NewResultFromResult(ipamResult)
		if err != nil {
			// TBD: CLEAN-UP 
			return err
		}

		if len(result.IPs) == 0  {
			// TBD: CLEAN-UP 
			return fmt.Errorf("ERROR: Unable to get IP Address")
		}

		// Map result to local usrtype structure.
		// TBD: Convert cnivpp to use new structure (result)
		//      This is here from when cnivpp was in its own repo and
		//      vendor issue with using different versions (different
		//      vendor directories) of IPAM.
		for _, ip := range result.IPs {
			if ip.Version == "4" {
				ipData.IsIpv6  = 0
				ipData.Address = ip.Address.IP.String()
				prefix, _ = ip.Address.Mask.Size()
				ipData.AddressLength = byte(prefix)
			} else if ip.Version == "6" {
				ipData.IsIpv6  = 1
				ipData.Address = ip.Address.IP.String()
				prefix, _ = ip.Address.Mask.Size()
				ipData.AddressLength = byte(prefix)
			}

			// Only one address is currently supported.
			if ipData.Address != "" {
				break
			}
		}

		// Clear out the Gateway if set by IPAM, not being used.
		for _, ip := range result.IPs {
			ip.Gateway = nil
		}

	}


	// Determine the Engine that will process the request. Default to host
	// if not provided.
	if netConf.ContainerConf.Engine != "" {
		containerEngine = netConf.ContainerConf.Engine
	} else {
		containerEngine = netConf.HostConf.Engine
	}
 
	// Add the requested interface and network
	if containerEngine == "vpp" {
		err = cnivpp.CniVppAddOnContainer(netConf, ipData, args.ContainerID)
		if err != nil {
			return err
		}
	} else if containerEngine == "ovs-dpdk" {
		return fmt.Errorf("GOOD: Found Container Engine:" + containerEngine + " - NOT SUPPORTED")
	} else {
		return fmt.Errorf("ERROR: Unknown Container Engine:" + containerEngine)
	}

	return  cnitypes.PrintResult(result, netConf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	var netConf *usrsptypes.NetConf
	var containerEngine string

	// Convert the input bytestream into local NetConf structure
	netConf, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}


	//
	// HOST:
	//

	// Delete the requested interface
	if netConf.HostConf.Engine == "vpp" {
		err = cnivpp.CniVppDelFromHost(netConf, args.ContainerID)
		if err != nil {
			return err
		}
	} else if netConf.HostConf.Engine == "ovs-dpdk" {
		return fmt.Errorf("GOOD: Found Host Engine:" + netConf.HostConf.Engine + " - NOT SUPPORTED")
	} else {
		return fmt.Errorf("ERROR: Unknown Host Engine:" + netConf.HostConf.Engine)
	}


	//
	// CONTAINER
	//

	// Determine the Engine that will process the request. Default to host
	// if not provided.
	if netConf.ContainerConf.Engine != "" {
		containerEngine = netConf.ContainerConf.Engine
	} else {
		containerEngine = netConf.HostConf.Engine
	}

	// Delete the requested interface
	if containerEngine == "vpp" {
		err = cnivpp.CniVppDelFromContainer(netConf, args.ContainerID)
		if err != nil {
			return err
		}
	} else if containerEngine == "ovs-dpdk" {
		return fmt.Errorf("GOOD: Found Container Engine:" + containerEngine + " - NOT SUPPORTED")
	} else {
		return fmt.Errorf("ERROR: Unknown Container Engine:" + containerEngine)
	}


	//
	// Cleanup IPAM data, if provided.
	//
	if netConf.IPAM.Type != "" {
		err = ipam.ExecDel(netConf.IPAM.Type, args.StdinData)
		if err != nil {
			return err
		}
	}

	//
	// Cleanup Namespace
	//
	if args.Netns == "" {
		return nil
	}

	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		var err error
		_, err = ip.DelLinkByNameAddr(args.IfName, netlink.FAMILY_V4)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdDel, cniSpecVersion.All)
}
