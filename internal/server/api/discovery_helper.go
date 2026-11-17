package api

import "github.com/pcim/pc-inventory-manager/internal/server/discovery"

func discoveryLocalSubnet() string { return discovery.LocalSubnet() }
