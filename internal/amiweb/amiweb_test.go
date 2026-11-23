package amiweb

import "testing"

// Replies below are verbatim from the Quanta in Tested Hardware (AMI MegaRAC
// SP, FW 7.54), captured 2026-11-23; the copyright banner is trimmed.

func TestParseRPC(t *testing.T) {
	recs, status, err := parseRPC([]byte(` WEBVAR_JSONVAR_HL_GETVMEDIACFG = 
 { 
 WEBVAR_STRUCTNAME_GETVMEDIACFG : 
 [ 
 { 'V_CD_STATUS' : 1,'V_MAX_CD_SESSIONS' : 129,'V_STR_CD_PORT' : 5120,'V_STR_CD_SECURE_PORT' : 5124 },  {} ],  
 HAPI_STATUS:0 }; `))
	if err != nil || status != 0 || len(recs) != 1 || recs[0]["V_STR_CD_PORT"] != "5120" || recs[0]["V_MAX_CD_SESSIONS"] != "129" {
		t.Fatalf("recs=%v status=%d err=%v", recs, status, err)
	}

	recs, _, _ = parseRPC([]byte(` WEBVAR_JSONVAR_GETLMEDIAIMAGE = { WEBVAR_STRUCTNAME_GETLMEDIAIMAGE : [ { 'IMAGE_TYPE' : 2,'IMAGE_TYPE_STR' : 'Floppy','IMAGE_NAME' : '','IMAGE_EXISTS' : 0,'IMAGE_INFO' : '' },  { 'IMAGE_TYPE' : 1,'IMAGE_TYPE_STR' : 'CD/DVD','IMAGE_NAME' : '','IMAGE_EXISTS' : 0,'IMAGE_INFO' : '' },  { 'IMAGE_TYPE' : 4,'IMAGE_TYPE_STR' : 'Harddisk','IMAGE_NAME' : '','IMAGE_EXISTS' : 0,'IMAGE_INFO' : '' },  {} ],  HAPI_STATUS:0 };`))
	if len(recs) != 3 || recs[1]["IMAGE_TYPE_STR"] != "CD/DVD" || recs[1]["IMAGE_NAME"] != "" {
		t.Fatalf("lmedia recs %v", recs)
	}

	recs, status, _ = parseRPC([]byte(` WEBVAR_JSONVAR_WEB_SESSION = { WEBVAR_STRUCTNAME_WEB_SESSION : [ { 'SESSION_COOKIE' : 'Failure_Login_IPMI_Then_LDAP_then_Active_Directory_Radius' },  {} ],  HAPI_STATUS:-6 };`))
	if status != -6 || recs[0]["SESSION_COOKIE"][:7] != "Failure" {
		t.Fatalf("failed login: %v %d", recs, status)
	}

	if _, _, err := parseRPC([]byte("<html>404</html>")); err == nil {
		t.Fatal("non-RPC reply parsed")
	}
}

func TestLaunchFileArgs(t *testing.T) {
	// Argument layout of the BMC's JViewer launch file, token values replaced.
	b := []byte(`    <application-desc>
        <argument>-apptype</argument>
<argument>JViewer</argument>
<argument>-hostname</argument>
<argument>192.168.139.4</argument>
<argument>-kvmtoken</argument>
<argument>AbCdEf0123456789</argument>
<argument>-vmsecure</argument>
<argument>0</argument>
<argument>-cdport</argument>
<argument>5120</argument>
<argument>-webcookie</argument>
<argument>xyz</argument>
`)
	args := map[string]string{}
	for _, m := range argRE.FindAllSubmatch(b, -1) {
		args[string(m[1])] = string(m[2])
	}
	if args["-kvmtoken"] != "AbCdEf0123456789" || args["-cdport"] != "5120" || args["-vmsecure"] != "0" || args["-hostname"] != "192.168.139.4" {
		t.Fatalf("args %v", args)
	}
}
