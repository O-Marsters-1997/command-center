(function () {
  var chordWindowMs = 1500;
  var prefix = "";
  var prefixTimer = 0;

  function clearPrefix() {
    prefix = "";
    clearTimeout(prefixTimer);
  }

  function inField(el) {
    if (!el) return false;
    return el.matches("input, textarea, select, [contenteditable]");
  }

  function isMac() {
    return /Mac|iPhone|iPad/.test(navigator.platform);
  }

  function keyName(e) {
    var k = e.key;
    if (k === "Escape") return "escape";
    if (k === "Enter") return "enter";
    return k.toLowerCase();
  }

  function press(binding) {
    var matches = document.querySelectorAll('[data-key="' + binding + '"]');
    var target = null;
    matches.forEach(function (el) {
      if (!target || (el.closest("#main") && !target.closest("#main"))) target = el;
    });
    if (!target) return false;
    if (target.matches("input, textarea, select")) target.focus();
    else target.click();
    return true;
  }

  document.addEventListener("keydown", function (e) {
    if (e.altKey || e.isComposing) return;
    var mod = isMac() ? e.metaKey : e.ctrlKey;
    var otherMod = isMac() ? e.ctrlKey : e.metaKey;
    var name = keyName(e);
    if (otherMod) return;
    if (mod && name !== "k") return;

    var focused = document.activeElement;
    if (inField(focused) && !(mod && name === "k") && name !== "escape") return;
    if (name === "enter" && focused && focused.matches("a, button, summary")) return;

    var binding = mod ? "mod+" + name : name;
    if (prefix) {
      var chord = prefix + " " + binding;
      clearPrefix();
      if (press(chord)) e.preventDefault();
      return;
    }
    if (binding === "g") {
      prefix = "g";
      prefixTimer = setTimeout(clearPrefix, chordWindowMs);
      return;
    }
    if (press(binding)) e.preventDefault();
  });
})();
