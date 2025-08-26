package pkg

import "testing"

func TestIsAnsibleVaultString(t *testing.T) {
	vaultStr := `!vault |
      $ANSIBLE_VAULT;1.1;AES256
      62313365396662343061393464336163383764373764613633653634306231386433626436623361
      6134333665353966363534333632666535333761666131620a663537646436643839616531643561
      63396265333966386166373632626539326166353965363262633030333630313338646335303630
      3438626666666137650a353638643435666633633964366338633066623234616432373231333331
      6564`

	if !IsAnsibleVaultString(vaultStr) {
		t.Error("Expected string to be identified as Ansible vault string")
	}

	nonVaultStr := "regular string"
	if IsAnsibleVaultString(nonVaultStr) {
		t.Error("Expected regular string to not be identified as Ansible vault string")
	}
}

func TestNewAnsibleVaultString_Version1_1(t *testing.T) {
	vaultStr := `!vault |
      $ANSIBLE_VAULT;1.1;AES256
      62313365396662343061393464336163383764373764613633653634306231386433626436623361
      6134333665353966363534333632666535333761666131620a663537646436643839616531643561
      63396265333966386166373632626539326166353965363262633030333630313338646335303630
      3438626666666137650a353638643435666633633964366338633066623234616432373231333331
      6564`

	vault, err := NewAnsibleVaultString(vaultStr)
	if err != nil {
		t.Errorf("Failed to parse vault string: %v", err)
	}

	if vault.FormatId != "$ANSIBLE_VAULT" {
		t.Errorf("Expected format ID to be $ANSIBLE_VAULT, got %s", vault.FormatId)
	}

	if vault.Version != "1.1" {
		t.Errorf("Expected version to be 1.1, got %s", vault.Version)
	}

	if vault.VaultId != "" {
		t.Errorf("Expected vault ID to be empty, got %s", vault.VaultId)
	}

	expectedCipherText := "62313365396662343061393464336163383764373764613633653634306231386433626436623361" +
		"6134333665353966363534333632666535333761666131620a663537646436643839616531643561" +
		"63396265333966386166373632626539326166353965363262633030333630313338646335303630" +
		"3438626666666137650a353638643435666633633964366338633066623234616432373231333331" +
		"6564"

	if vault.CipherText != expectedCipherText {
		t.Errorf("Ciphertext does not match expected value. Got %s, expected %s", vault.CipherText, expectedCipherText)
	}
}

func TestNewAnsibleVaultString(t *testing.T) {
	vaultStr := `!vault |
      $ANSIBLE_VAULT;1.1;AES256
      62313365396662343061393464336163383764373764613633653634306231386433626436623361
      6134333665353966363534333632666535333761666131620a663537646436643839616531643561
      63396265333966386166373632626539326166353965363262633030333630313338646335303630
      3438626666666137650a353638643435666633633964366338633066623234616432373231333331
      6564`

	vault, err := NewAnsibleVaultString(vaultStr)
	if err != nil {
		t.Errorf("Failed to parse vault string: %v", err)
	}

	if vault.FormatId != "$ANSIBLE_VAULT" {
		t.Errorf("Expected format ID to be $ANSIBLE_VAULT, got %s", vault.FormatId)
	}

	if vault.Version != "1.1" {
		t.Errorf("Expected version to be 1.1, got %s", vault.Version)
	}

	expectedCipherText := "62313365396662343061393464336163383764373764613633653634306231386433626436623361" +
		"6134333665353966363534333632666535333761666131620a663537646436643839616531643561" +
		"63396265333966386166373632626539326166353965363262633030333630313338646335303630" +
		"3438626666666137650a353638643435666633633964366338633066623234616432373231333331" +
		"6564"

	if vault.CipherText != expectedCipherText {
		t.Errorf("Ciphertext does not match expected value. Got %s, expected %s", vault.CipherText, expectedCipherText)
	}

	// Test invalid input
	invalidStr := "not a vault string"
	_, err = NewAnsibleVaultString(invalidStr)
	if err == nil {
		t.Error("Expected error when parsing invalid vault string")
	}

	invalidVaultStr := `!vault |
      $ANSIBLE_VAULT`
	_, err = NewAnsibleVaultString(invalidVaultStr)
	if err == nil {
		t.Error("Expected error when parsing vault string with missing parts")
	}
}

func TestEncrypt(t *testing.T) {
	plaintext := "test secret"
	password := "mypassword"

	vault, err := Encrypt(plaintext, password)
	if err != nil {
		t.Errorf("Failed to encrypt: %v", err)
	}

	if vault.FormatId != "$ANSIBLE_VAULT" {
		t.Errorf("Expected format ID to be $ANSIBLE_VAULT, got %s", vault.FormatId)
	}

	if vault.Version != "1.1" {
		t.Errorf("Expected version to be 1.1, got %s", vault.Version)
	}

	if vault.VaultId != "default" {
		t.Errorf("Expected vault ID to be default, got %s", vault.VaultId)
	}

	// Verify we can decrypt it back
	decrypted, err := vault.Decrypt(password)
	if err != nil {
		t.Errorf("Failed to decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Expected decrypted text to be %q, got %q", plaintext, decrypted)
	}

	// Test with wrong password
	_, err = vault.Decrypt("wrongpassword")
	if err == nil {
		t.Error("Expected error when decrypting with wrong password")
	}
}

func TestDecrypt(t *testing.T) {
	// This is created with `ansible-vault encrypt_string`
	// DO NOT MODIFY THIS STRING, THIS IS THE OUTPUT THAT SHOULD BE COMPATIBLE
	// WITH THE CODE THAT WE HAVE.
	cipherText := `!vault |
          $ANSIBLE_VAULT;1.1;AES256
          66623566633261383666353136643131366331623562333130303432646333653362306330363830
          3933616430336334326338373437323064353236633162630a396461393665326234343263663533
          62383864336338343438366537623234356633333664396335336533323365626637333166646438
          3063666238303462340a613337346263393230303534616434653938313566616262373465353965
          3837`
	password := "test"

	vault, err := NewAnsibleVaultString(cipherText)
	if err != nil {
		t.Errorf("Failed to parse: %v", err)
	}

	decrypted, err := vault.Decrypt(password)
	if err != nil {
		t.Errorf("Failed to decrypt: %v", err)
	}

	if decrypted != "dummy" {
		t.Errorf("Expected decrypted text to be %q, got %q", "dummy", decrypted)
	}
}
