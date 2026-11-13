package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// LDAP 模擬伺服器：以 Testcontainers 啟動 OpenLDAP，讓真正的 AD 驗證模組（go-ldap）以服務帳號繫結、
// 依 sAMAccountName 搜尋 objectClass=user、檢查 memberOf，再以使用者的 DN 繫結驗證密碼。
//
// 這是「AD 形狀的 OpenLDAP」，不是真正的 Active Directory：
//   - 只借用映像裡的 slapd，不使用映像自己的初始化流程；以自備的 slapd.conf、schema 與 LDIF 啟動。
//   - user 類別與 sAMAccountName、memberOf、userPrincipalName 由下方的 ldapSimADSchema 定義，名稱與 OID 與 AD 相同。
//     Debian 套件內建的 msuser.schema 沒有為 sAMAccountName 定義比對規則，依它建立的目錄無法用
//     (sAMAccountName=...) 搜尋，所以不採用。
//   - memberOf 直接寫在使用者項目上，不由伺服器依群組成員推算。
//
// 與實際 AD 的差異（巢狀群組、帳號停用、密碼過期等）不在模擬範圍，仍需以真正的 AD 驗證。

// ldapSimImage 鎖定映像的 digest。對應 vegardit/openldap:latest（2026-11-11 建置，OpenLDAP 2.6.10）。
const ldapSimImage = "vegardit/openldap@sha256:733da245adb11a43aa4282eb92d4d6e1cec286bac46aea2ef6c8614c8e9412db"

const (
	ldapSimBaseDN       = "dc=campus,dc=test"
	ldapSimBindDN       = "cn=svc-wurzburg,ou=Service," + ldapSimBaseDN
	ldapSimBindPassword = "SimLdapBindPass#41"
	ldapSimAllowedGroup = "cn=ip-users,ou=Groups," + ldapSimBaseDN
	ldapSimRootPassword = "SimLdapRootPass#41"
)

// ldapSimUser 是目錄中的一個組織帳號。InGroup 表示是否屬於允許登入的群組。
type ldapSimUser struct {
	Username    string
	Password    string
	DisplayName string
	Mail        string
	InGroup     bool
}

type ldapSim struct {
	container testcontainers.Container
	url       string // ldap://127.0.0.1:<port>
}

// ldapSimADSchema 定義 AD 形狀的使用者類別；名稱與 OID 取自 Active Directory schema。
const ldapSimADSchema = `attributetype ( 1.2.840.113556.1.4.221 NAME 'sAMAccountName'
	EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch
	SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 SINGLE-VALUE )
attributetype ( 1.2.840.113556.1.4.656 NAME 'userPrincipalName'
	EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch
	SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 SINGLE-VALUE )
attributetype ( 1.2.840.113556.1.2.102 NAME 'memberOf'
	EQUALITY distinguishedNameMatch
	SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 )
objectclass ( 1.2.840.113556.1.5.9 NAME 'user'
	SUP organizationalPerson STRUCTURAL
	MUST sAMAccountName
	MAY ( displayName $ mail $ memberOf $ userPrincipalName ) )
`

const ldapSimConf = `include /etc/ldap/schema/core.schema
include /etc/ldap/schema/cosine.schema
include /etc/ldap/schema/inetorgperson.schema
include /sim/ad.schema
modulepath /usr/lib/ldap
moduleload back_mdb
pidfile /sim/slapd.pid

database mdb
suffix "` + ldapSimBaseDN + `"
rootdn "cn=root,` + ldapSimBaseDN + `"
rootpw ` + ldapSimRootPassword + `
directory /sim/db
maxsize 268435456
index objectClass,sAMAccountName eq

# 密碼只能用來繫結，任何人都讀不到；目錄內容只有服務帳號可以搜尋，匿名連線只能繫結。
access to attrs=userPassword by anonymous auth by * none
access to * by dn.exact="` + ldapSimBindDN + `" read by anonymous auth by * none
`

func ldapSimLDIF(users []ldapSimUser) []byte {
	var b bytes.Buffer
	b.WriteString("dn: " + ldapSimBaseDN + "\nobjectClass: top\nobjectClass: dcObject\nobjectClass: organization\ndc: campus\no: Campus Test\n\n")
	for _, ou := range []string{"Service", "Groups", "People"} {
		fmt.Fprintf(&b, "dn: ou=%s,%s\nobjectClass: organizationalUnit\nou: %s\n\n", ou, ldapSimBaseDN, ou)
	}
	fmt.Fprintf(&b, "dn: %s\nobjectClass: person\ncn: svc-wurzburg\nsn: service\nuserPassword: %s\n\n", ldapSimBindDN, ldapSimBindPassword)

	var members []string
	for _, u := range users {
		dn := fmt.Sprintf("cn=%s,ou=People,%s", u.Username, ldapSimBaseDN)
		fmt.Fprintf(&b, "dn: %s\nobjectClass: top\nobjectClass: person\nobjectClass: organizationalPerson\nobjectClass: user\n", dn)
		fmt.Fprintf(&b, "cn: %s\nsn: %s\nsAMAccountName: %s\ndisplayName:: %s\nmail: %s\nuserPassword: %s\n",
			u.Username, u.Username, u.Username, base64Std(u.DisplayName), u.Mail, u.Password)
		if u.InGroup {
			fmt.Fprintf(&b, "memberOf: %s\n", ldapSimAllowedGroup)
			members = append(members, dn)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "dn: %s\nobjectClass: groupOfNames\ncn: ip-users\n", ldapSimAllowedGroup)
	if len(members) == 0 {
		members = []string{ldapSimBindDN} // groupOfNames 至少要有一個成員
	}
	for _, m := range members {
		fmt.Fprintf(&b, "member: %s\n", m)
	}
	b.WriteString("\n")
	return b.Bytes()
}

// startLDAPSim 啟動 LDAP 模擬伺服器並載入指定的組織帳號。
func startLDAPSim(t *testing.T, users []ldapSimUser) *ldapSim {
	t.Helper()
	ctx := context.Background()
	script := strings.Join([]string{
		"mkdir -p /sim/db",
		"slapadd -f /sim/slapd.conf -l /sim/data.ldif",
		"exec slapd -f /sim/slapd.conf -h ldap://0.0.0.0:389/ -d stats",
	}, " && ")
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        ldapSimImage,
			ExposedPorts: []string{"389/tcp"},
			Cmd:          []string{"/bin/sh", "-c", script},
			Files: []testcontainers.ContainerFile{
				{Reader: strings.NewReader(ldapSimConf), ContainerFilePath: "/sim/slapd.conf", FileMode: 0o644},
				{Reader: strings.NewReader(ldapSimADSchema), ContainerFilePath: "/sim/ad.schema", FileMode: 0o644},
				{Reader: bytes.NewReader(ldapSimLDIF(users)), ContainerFilePath: "/sim/data.ldif", FileMode: 0o644},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("slapd starting"),
				wait.ForListeningPort("389/tcp"),
			).WithDeadline(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("啟動 LDAP 模擬伺服器失敗: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	port, err := c.MappedPort(ctx, "389/tcp")
	if err != nil {
		t.Fatalf("取得 LDAP 模擬伺服器的埠失敗: %v", err)
	}
	return &ldapSim{container: c, url: fmt.Sprintf("ldap://127.0.0.1:%d", port.Num())}
}

// stop 終止 LDAP 模擬伺服器，模擬驗證來源中斷。
func (s *ldapSim) stop(t *testing.T) {
	t.Helper()
	if err := testcontainers.TerminateContainer(s.container); err != nil {
		t.Fatalf("終止 LDAP 模擬伺服器失敗: %v", err)
	}
}

func base64Std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
