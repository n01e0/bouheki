package network

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/aquasecurity/libbpfgo"
	"github.com/mrtc0/bouheki/pkg/config"
	log "github.com/mrtc0/bouheki/pkg/log"
)

const (
	MODE_MONITOR uint32 = 0
	MODE_BLOCK   uint32 = 1

	TARGET_HOST      uint32 = 0
	TAREGT_CONTAINER uint32 = 1

	// BPF Map Names
	BOUHEKI_CONFIG_MAP_NAME       = "bouheki_config"
	ALLOWED_CIDR_LIST_MAP_NAME    = "allowed_cidr_list"
	DENIED_CIDR_LIST_MAP_NAME     = "denied_cidr_list"
	ALLOWED_UID_LIST_MAP_NAME     = "allowed_uid_list"
	DENIED_UID_LIST_MAP_NAME      = "denied_uid_list"
	ALLOWED_GID_LIST_MAP_NAME     = "allowed_gid_list"
	DENIED_GID_LIST_MAP_NAME      = "denied_gid_list"
	ALLOWED_COMMAND_LIST_MAP_NAME = "allowed_command_list"
	DENIED_COMMAND_LIST_MAP_NAME  = "denied_command_list"

	/*
	   +---------------+---------------+-------------------+-------------------+-------------------+
	   | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12  | 13 | 14 | 15 | 16 | 17 | 18 | 19 | 20 |
	   +---------------+---------------+-------------------+-------------------+-------------------+
	   |      MODE     |     TARGET    | Allow Command Size|  Allow UID Size   | Allow GID Size    |
	   +---------------+---------------+-------------------+-------------------+-------------------+
	*/

	MAP_SIZE                = 20
	MAP_MODE_START          = 0
	MAP_MODE_END            = 4
	MAP_TARGET_START        = 4
	MAP_TARGET_END          = 8
	MAP_ALLOW_COMMAND_INDEX = 8
	MAP_ALLOW_UID_INDEX     = 12
	MAP_ALLOW_GID_INDEX     = 16
)

type Manager struct {
	mod    *libbpfgo.Module
	config *config.Config
	rb     *libbpfgo.RingBuffer
}

func (m *Manager) SetConfigToMap() error {
	if err := m.setConfigMap(); err != nil {
		return err
	}
	if err := m.setAllowedCIDRList(); err != nil {
		return err
	}
	if err := m.setDeniedCIDRList(); err != nil {
		return err
	}
	if err := m.setAllowedCommandList(); err != nil {
		return err
	}
	if err := m.setDeniedCommandList(); err != nil {
		return err
	}
	if err := m.setAllowedUIDList(); err != nil {
		return err
	}
	if err := m.setDeniedUIDList(); err != nil {
		return err
	}
	if err := m.setAllowedGIDList(); err != nil {
		return err
	}
	if err := m.setDeniedGIDList(); err != nil {
		return err
	}
	if err := m.attach(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Start(eventsChannel chan []byte) error {
	rb, err := m.mod.InitRingBuf("audit_events", eventsChannel)

	if err != nil {
		return err
	}

	rb.Start()
	m.rb = rb

	return nil
}

func (m *Manager) Close() {
	m.rb.Close()
}

func (m *Manager) attach() error {
	programs := []string{"socket_connect"}
	for _, progName := range programs {
		prog, err := m.mod.GetProgram(progName)

		if err != nil {
			return err
		}

		_, err = prog.AttachLSM()
		if err != nil {
			return err
		}

		log.Debug(fmt.Sprintf("%s attached.", progName))
	}

	return nil
}

func (m *Manager) setMode(table *libbpfgo.BPFMap, key []byte) []byte {
	if m.config.IsRestricted() {
		binary.LittleEndian.PutUint32(key[MAP_MODE_START:MAP_MODE_END], MODE_BLOCK)
	} else {
		binary.LittleEndian.PutUint32(key[MAP_MODE_START:MAP_MODE_END], MODE_MONITOR)
	}

	return key
}

func (m *Manager) setTarget(table *libbpfgo.BPFMap, key []byte) []byte {
	if m.config.IsOnlyContainer() {
		binary.LittleEndian.PutUint32(key[MAP_TARGET_START:MAP_TARGET_END], TAREGT_CONTAINER)
	} else {
		binary.LittleEndian.PutUint32(key[MAP_TARGET_START:MAP_TARGET_END], TARGET_HOST)
	}

	return key
}

func (m *Manager) setConfigMap() error {
	configMap, err := m.mod.GetMap(BOUHEKI_CONFIG_MAP_NAME)
	if err != nil {
		return err
	}

	key := make([]byte, MAP_SIZE)

	key = m.setMode(configMap, key)
	key = m.setTarget(configMap, key)

	binary.LittleEndian.PutUint32(key[MAP_ALLOW_COMMAND_INDEX:MAP_ALLOW_COMMAND_INDEX+4], uint32(len(m.config.Network.Command.Allow)))
	binary.LittleEndian.PutUint32(key[MAP_ALLOW_UID_INDEX:MAP_ALLOW_UID_INDEX+4], uint32(len(m.config.Network.UID.Allow)))
	binary.LittleEndian.PutUint32(key[MAP_ALLOW_GID_INDEX:MAP_ALLOW_GID_INDEX+4], uint32(len(m.config.Network.GID.Allow)))

	err = configMap.Update(uint8(0), key)

	if err != nil {
		return err
	}

	return nil
}

func (m *Manager) setAllowedCommandList() error {
	commands, err := m.mod.GetMap(ALLOWED_COMMAND_LIST_MAP_NAME)
	if err != nil {
		return err
	}

	for _, c := range m.config.Network.Command.Allow {
		err = commands.Update(byteToKey([]byte(c)), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setDeniedCommandList() error {
	commands, err := m.mod.GetMap(DENIED_COMMAND_LIST_MAP_NAME)
	if err != nil {
		return err
	}

	for _, c := range m.config.Network.Command.Deny {
		err = commands.Update(byteToKey([]byte(c)), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setAllowedUIDList() error {
	uids, err := m.mod.GetMap(ALLOWED_UID_LIST_MAP_NAME)
	if err != nil {
		return err
	}
	for _, uid := range m.config.Network.UID.Allow {
		err = uids.Update(uintToKey(uid), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setDeniedUIDList() error {
	uids, err := m.mod.GetMap(DENIED_UID_LIST_MAP_NAME)
	if err != nil {
		return err
	}
	for _, uid := range m.config.Network.UID.Deny {
		err = uids.Update(uintToKey(uid), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setAllowedGIDList() error {
	gids, err := m.mod.GetMap(ALLOWED_GID_LIST_MAP_NAME)
	if err != nil {
		return err
	}
	for _, gid := range m.config.Network.GID.Allow {
		err = gids.Update(uintToKey(gid), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setDeniedGIDList() error {
	gids, err := m.mod.GetMap(DENIED_UID_LIST_MAP_NAME)
	if err != nil {
		return err
	}
	for _, gid := range m.config.Network.GID.Deny {
		err = gids.Update(uintToKey(gid), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setAllowedCIDRList() error {
	allowed_cidr_list, err := m.mod.GetMap(ALLOWED_CIDR_LIST_MAP_NAME)
	if err != nil {
		return err
	}

	for _, s := range m.config.Network.CIDR.Allow {
		allowAddresses, err := parseCIDR(s)
		if err != nil {
			return err
		}
		err = allowed_cidr_list.Update(ipToKey(*allowAddresses), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) setDeniedCIDRList() error {
	denied_cidr_list, err := m.mod.GetMap(DENIED_CIDR_LIST_MAP_NAME)
	if err != nil {
		return err
	}

	for _, s := range m.config.Network.CIDR.Deny {
		denyAddresses, err := parseCIDR(s)
		if err != nil {
			return err
		}
		err = denied_cidr_list.Update(ipToKey(*denyAddresses), uint8(0))
		if err != nil {
			return err
		}
	}

	return nil
}

func ipToKey(n net.IPNet) []byte {
	prefixLen, _ := n.Mask.Size()

	key := make([]byte, 16)

	binary.LittleEndian.PutUint32(key[0:4], uint32(prefixLen))
	copy(key[4:], n.IP)

	return key
}

func byteToKey(b []byte) []byte {
	key := make([]byte, 16)
	copy(key[0:], b)
	return key
}

func uintToKey(i uint) []byte {
	key := make([]byte, 4)
	binary.LittleEndian.PutUint32(key[0:4], uint32(i))
	return key
}

func parseCIDR(cidr string) (*net.IPNet, error) {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	return n, nil
}
