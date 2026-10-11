(function () {
  var chordWindowMs = 1500;
  var prefix = "";
  var prefixTimer = 0;

  function clearPrefix() {
    prefix = "";
    clearTimeout(prefixTimer);
  }

  function inField(el) {
    return !!el && (el.matches("input, textarea, select") || el.isContentEditable);
  }

  function isMac() {
    return /Mac|iPhone|iPad/.test(navigator.platform);
  }

  function usable(el) {
    return !el.disabled && el.getClientRects().length > 0;
  }

  function press(binding) {
    var target = null;
    document.querySelectorAll("[data-key]").forEach(function (el) {
      if (el.dataset.key !== binding || !usable(el)) return;
      if (!target || (el.closest("#main") && !target.closest("#main"))) target = el;
    });
    if (!target) return false;
    if (target.matches("input, textarea, select")) target.focus();
    else target.click();
    return true;
  }

  document.addEventListener("keydown", function (e) {
    if (e.altKey || e.isComposing || e.repeat) return;
    if (e.shiftKey && /^[a-z]$/i.test(e.key)) return;
    var mac = isMac();
    var mod = mac ? e.metaKey : e.ctrlKey;
    if (mac ? e.ctrlKey : e.metaKey) return;
    var name = e.key.toLowerCase();
    if (mod && name !== "k") return;

    var focused = document.activeElement;
    if (inField(focused) && !mod && name !== "escape") return;
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

  document.addEventListener("focusin", function (e) {
    if (inField(e.target)) clearPrefix();
  });
})();
