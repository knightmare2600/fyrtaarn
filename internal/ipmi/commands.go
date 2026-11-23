package ipmi

// Placeholder IPMI command layer

// ColdResetBMC restarts the BMC itself with the IPMI Cold Reset command
// ("mc reset cold"); the server it manages keeps running. The BMC is
// unreachable for a minute or two afterwards. Used on the iLO 4 in Tested
// Hardware on 2026-11-23 to recover its stalled web server.
func ColdResetBMC(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "mc", "reset", "cold")
	return err
}
