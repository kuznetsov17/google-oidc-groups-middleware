// Licensed to Andrew Kroh under one or more agreements.
// Andrew Kroh licenses this file to you under the Apache 2.0 License.
// See the LICENSE file in the project root for more information.

package google_oidc_groups_middleware

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type serviceAccountKey struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	ClientID    string `json:"client_id"`
	AuthURI     string `json:"auth_uri"`
	TokenURI    string `json:"token_uri"`
}

type groupsFetcher struct {
	saEmail    string
	privateKey *rsa.PrivateKey
	tokenURI   string
	subject    string
}

func newGroupsFetcher(saJSON, subject string) (*groupsFetcher, error) {
	var sa serviceAccountKey
	if err := json.Unmarshal([]byte(saJSON), &sa); err != nil {
		return nil, fmt.Errorf("failed to parse service account JSON: %w", err)
	}

	// Parse PEM-encoded private key
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM private key")
	}

	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}

	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	return &groupsFetcher{
		saEmail:    sa.ClientEmail,
		privateKey: rsaKey,
		tokenURI:   sa.TokenURI,
		subject:    subject,
	}, nil
}

// getAccessToken creates a JWT assertion and exchanges it for an access token.
func (f *groupsFetcher) getAccessToken() (string, error) {
	now := time.Now()
	exp := now.Add(time.Hour)

	claims := map[string]interface{}{
		"iss": f.saEmail,
		"sub": f.subject,
		"scope": "https://www.googleapis.com/auth/admin.directory.group.readonly",
		"aud": f.tokenURI,
		"iat": now.Unix(),
		"exp": exp.Unix(),
	}

	token, err := f.createJWT(claims)
	if err != nil {
		return "", fmt.Errorf("failed to create JWT: %w", err)
	}

	// Exchange JWT for access token
	resp, err := http.PostForm(f.tokenURI, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {token},
	})
	if err != nil {
		return "", fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	return tokenResp.AccessToken, nil
}

// createJWT creates a signed JWT assertion for service account auth.
func (f *groupsFetcher) createJWT(claims map[string]interface{}) (string, error) {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}

	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	message := headerB64 + "." + claimsB64

	// Sign with RSA-SHA256
	digest := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return message + "." + sigB64, nil
}

// fetchGroups retrieves all Google Group names for the given user email using the Admin API.
// Handles pagination to retrieve all groups.
func (f *groupsFetcher) fetchGroups(userEmail string) ([]string, error) {
	token, err := f.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	var groups []string
	pageToken := ""

	for {
		groups, pageToken, err = f.listGroups(token, userEmail, pageToken)
		if err != nil {
			return nil, err
		}

		if pageToken == "" {
			break
		}
	}

	return groups, nil
}

// listGroups retrieves a single page of groups. Returns accumulated groups, nextPageToken, and any error.
func (f *groupsFetcher) listGroups(token, userEmail, pageToken string) ([]string, string, error) {
	u := "https://admin.googleapis.com/admin/directory/v1/groups"
	q := url.Values{
		"userKey":    {userEmail},
		"maxResults": {"200"},
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}

	req, err := http.NewRequest("GET", u+"?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list groups: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("groups endpoint returned %d: %s", resp.StatusCode, body)
	}

	var groupsResp struct {
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
		NextPageToken string `json:"nextPageToken"`
	}

	if err := json.Unmarshal(body, &groupsResp); err != nil {
		return nil, "", fmt.Errorf("failed to parse groups response: %w", err)
	}

	var names []string
	for _, g := range groupsResp.Groups {
		names = append(names, g.Name)
	}

	return names, groupsResp.NextPageToken, nil
}

// isGroupMember returns true if the user is in at least one of the allowed groups.
func isGroupMember(userGroups []string, allowedGroups map[string]struct{}) bool {
	if allowedGroups == nil {
		return true // No groups configured, allow all
	}
	for _, g := range userGroups {
		if _, ok := allowedGroups[g]; ok {
			return true
		}
	}
	return false
}
