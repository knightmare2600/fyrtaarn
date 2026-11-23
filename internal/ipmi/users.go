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
	users := parseUserList(output)

	// "user list" has no enabled column, so parseUserList guesses from the
	// name. "channel getaccess" reports each user's real Enable Status
	// (Get User Access; ipmi_channel.c). Channel 14 means "the channel this
	// request arrived on" in the IPMI spec, so no per-vendor LAN channel
	// number is needed. If it fails, the guess stands.
	if acc, err := runIPMICommand(host, user, pass, ipmiLongTimeout, "channel", "getaccess", "14"); err == nil {
		applyEnableStatus(users, parseEnableStatus(acc))
	}
	return users, nil
}

// parseEnableStatus reads "channel getaccess" output into user ID ->
// enabled, for users whose status is "enabled" or "disabled" ("unknown"
// means the BMC does not track it).
func parseEnableStatus(out string) map[int]bool {
	status := map[int]bool{}
	id := -1
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "User ID":
			if n, err := strconv.Atoi(v); err == nil {
				id = n
			}
		case "Enable Status":
			switch v {
			case "enabled":
				status[id] = true
			case "disabled":
				status[id] = false
			}
		}
	}
	return status
}

func applyEnableStatus(users []UserEntry, status map[int]bool) {
	for i := range users {
		if on, ok := status[users[i].ID]; ok {
			users[i].Enabled = on
		}
	}
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
	// Omitting the password makes ipmitool prompt for it twice (getpass),
	// keeping it out of argv; see runIPMIPrompted.
	_, err := runIPMIPrompted(host, adminUser, adminPass, ipmiShortTimeout,
		[]string{newPass, newPass},
		"user", "set", "password", strconv.Itoa(userID))
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
//
// When a slot has no name ipmitool leaves that column blank, so
// strings.Fields collapses it: fields[1] becomes the Callin value
// ("true"/"false") instead of the name. Detect this by checking whether
// fields[1] is a boolean, and adjust field indices accordingly.
func parseUserList(data string) []UserEntry {
	var entries []UserEntry

	for _, line := range strings.Split(data, "\n") {
		// Skip header line and blank lines.
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		// Columns: ID, name, Callin, Link Auth, IPMI Msg, privilege. The name
		// may be empty or contain spaces (iLO 5 names unused slots
		// "(Empty User)"), so locate the three true/false columns and take
		// the name as everything between the ID and them.
		isBool := func(s string) bool { return strings.EqualFold(s, "true") || strings.EqualFold(s, "false") }
		b := -1
		for i := 1; i+3 < len(fields); i++ {
			if isBool(fields[i]) && isBool(fields[i+1]) && isBool(fields[i+2]) {
				b = i
				break
			}
		}
		if b < 0 {
			continue
		}
		name := strings.Join(fields[1:b], " ")
		callinF, linkF, ipmiF := fields[b], fields[b+1], fields[b+2]
		privilege := strings.Join(fields[b+3:], " ")

		entries = append(entries, UserEntry{
			ID:            id,
			Name:          name,
			CallinEnabled: strings.EqualFold(callinF, "true"),
			LinkEnabled:   strings.EqualFold(linkF, "true"),
			IPMIEnabled:   strings.EqualFold(ipmiF, "true"),
			// A guess: "user list" has no enabled column. GetUsers replaces
			// it with the BMC's own Enable Status where it reports one.
			Enabled:   name != "" && name != "(Empty User)",
			Privilege: privilege,
		})
	}

	return entries
}
