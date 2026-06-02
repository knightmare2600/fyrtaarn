package ipmi

import (
	"fmt"
	"strconv"
	"strings"
)

type UserEntry struct {
	ID      int
	Name    string
	Enabled bool
	// Callin / Link / IPMI messaging privileges as reported by ipmitool
	CallinEnabled bool
	LinkEnabled   bool
	IPMIEnabled   bool
	// Channel privilege level: CALLBACK, USER, OPERATOR, ADMINISTRATOR, OEM
	Privilege string
}

func GetUsers(host, user, pass string) ([]UserEntry, error) {
	output, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "user", "list")
	if err != nil {
		return nil, err
	}
	return parseUserList(output), nil
}

func EnableUser(host, adminUser, adminPass string, userID int) error {
	_, err := runIPMICommand(host, adminUser, adminPass, ipmiShortTimeout,
		"user", "enable", strconv.Itoa(userID))
	return err
}

func DisableUser(host, adminUser, adminPass string, userID int) error {
	_, err := runIPMICommand(host, adminUser, adminPass, ipmiShortTimeout,
		"user", "disable", strconv.Itoa(userID))
	return err
}

func SetUserPassword(host, adminUser, adminPass string, userID int, newPass string) error {
	defer wipeString(&newPass)
	_, err := runIPMICommand(host, adminUser, adminPass, ipmiShortTimeout,
		"user", "set", "password", strconv.Itoa(userID), newPass)
	return err
}

func SetUserName(host, adminUser, adminPass string, userID int, name string) error {
	_, err := runIPMICommand(host, adminUser, adminPass, ipmiShortTimeout,
		"user", "set", "name", strconv.Itoa(userID), name)
	return err
}

// CreateUser provisions a previously empty slot: sets name, password,
// enables the account, and applies the channel privilege level.
// privilege: 2=USER, 3=OPERATOR, 4=ADMINISTRATOR, 5=OEM
func CreateUser(host, adminUser, adminPass string, userID int, name, password string, privilege int) error {
	defer wipeString(&password)
	if err := SetUserName(host, adminUser, adminPass, userID, name); err != nil {
		return fmt.Errorf("set name: %w", err)
	}
	if err := SetUserPassword(host, adminUser, adminPass, userID, password); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if err := EnableUser(host, adminUser, adminPass, userID); err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	if err := SetUserPrivilege(host, adminUser, adminPass, userID, 1, privilege); err != nil {
		return fmt.Errorf("set privilege: %w", err)
	}
	return nil
}

// DeleteUser disables a user account and clears its name, effectively freeing
// the slot. IPMI has no true delete command; this is the standard approach.
func DeleteUser(host, adminUser, adminPass string, userID int) error {
	if err := DisableUser(host, adminUser, adminPass, userID); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	return SetUserName(host, adminUser, adminPass, userID, "")
}

// SetUserPrivilege sets the channel privilege level for a user.
// level: 1=CALLBACK, 2=USER, 3=OPERATOR, 4=ADMINISTRATOR
func SetUserPrivilege(host, adminUser, adminPass string, userID, channel, level int) error {
	_, err := runIPMICommand(host, adminUser, adminPass, ipmiShortTimeout,
		"channel", "setaccess", strconv.Itoa(channel),
		strconv.Itoa(userID),
		"link=on", "ipmi=on", "callin=on",
		fmt.Sprintf("privilege=%d", level))
	return err
}

// parseUserList parses "ipmitool user list" output.
// Format: ID  Name	Callin  Link Auth  IPMI Msg   Channel Priv Limit
func parseUserList(data string) []UserEntry {
	var entries []UserEntry

	for _, line := range strings.Split(data, "\n") {
		// Skip header line and blank lines.
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID") {
			continue
		}

		fields := strings.Fields(line)
		// Minimum: ID Name Callin Link IPMI Privilege
		if len(fields) < 6 {
			continue
		}

		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		// Privilege may be multi-word ("NO ACCESS") — join remaining fields.
		privilege := strings.Join(fields[5:], " ")

		entries = append(entries, UserEntry{
			ID:            id,
			Name:          fields[1],
			CallinEnabled: strings.EqualFold(fields[2], "true"),
			LinkEnabled:   strings.EqualFold(fields[3], "true"),
			IPMIEnabled:   strings.EqualFold(fields[4], "true"),
			Enabled:       fields[1] != "" && fields[1] != "(Empty User)",
			Privilege:     privilege,
		})
	}

	return entries
}
