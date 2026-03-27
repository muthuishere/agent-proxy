from proxy.vault import Vault


def test_mask_and_restore():
    vault = Vault()
    original = "sk-ant-api03-supersecretkey12345"
    placeholder = vault.mask(original, "ANTHROPIC_API_KEY")
    assert original not in placeholder
    assert vault.restore(placeholder) == original


def test_mask_shows_prefix_suffix():
    vault = Vault(prefix_chars=4, suffix_chars=4)
    value = "sk-ant-api03-supersecretXXXX"
    placeholder = vault.mask(value, "KEY")
    assert placeholder.startswith("sk-a")
    assert "XXXX" in placeholder


def test_restore_leaves_nonmatching_text():
    vault = Vault()
    vault.mask("secret123", "KEY")
    text = "hello world no secrets here"
    assert vault.restore(text) == text
