const STORAGE_KEY = "wolsender.devices.v1";

const state = {
  interfaces: [],
  devices: new Map(), // key -> device
  selectedKey: null,
  busy: false,
};

const el = {
  interfaceSelect: document.querySelector("#interface"),
  interfaceDetail: document.querySelector("#interface-detail"),
  interfaceCount: document.querySelector("#interface-count"),
  port: document.querySelector("#port"),
  refresh: document.querySelector("#refresh"),
  scan: document.querySelector("#scan"),
  scanStatus: document.querySelector("#scan-status"),
  scanSummary: document.querySelector("#scan-summary"),
  deviceList: document.querySelector("#device-list"),
  selected: document.querySelector("#selected"),
  selectedName: document.querySelector("#selected-name"),
  selectedBadge: document.querySelector("#selected-badge"),
  selectedDetail: document.querySelector("#selected-detail"),
  macMissing: document.querySelector("#mac-missing"),
  macInput: document.querySelector("#mac-input"),
  labelInput: document.querySelector("#label-input"),
  wakeSelected: document.querySelector("#wake-selected"),
  manualForm: document.querySelector("#manual-form"),
  manualMac: document.querySelector("#manual-mac"),
  status: document.querySelector("#status"),
};

// ---------- helpers ----------

function normalizeMac(input) {
  const hex = String(input).replace(/[^0-9a-fA-F]/g, "").toUpperCase();
  if (hex.length !== 12) return null;
  return hex.match(/.{2}/g).join(":");
}

function deviceKey(device) {
  return device.mac ? `mac:${device.mac}` : `ip:${device.ip}@${device.interfaceId}`;
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function deviceName(device) {
  return device.label || device.hostname || device.ip || "Unknown device";
}

function relativeSeen(iso) {
  if (!iso) return "never";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "unknown";
  const seconds = Math.round((Date.now() - then) / 1000);
  if (seconds < 60) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function setStatus(node, message, tone = "neutral") {
  node.textContent = message || "";
  node.dataset.tone = tone;
}

function setBusy(isBusy) {
  state.busy = isBusy;
  el.refresh.disabled = isBusy;
  el.scan.disabled = isBusy || !selectedInterface(); // scanning needs a concrete interface
  el.wakeSelected.disabled = isBusy || !canWakeSelected();
}

// ---------- persistence (localStorage only) ----------

function loadKnownDevices() {
  let saved = [];
  try {
    saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]");
  } catch {
    saved = [];
  }
  if (!Array.isArray(saved)) return;
  for (const device of saved) {
    if (!device || !device.mac) continue;
    state.devices.set(deviceKey(device), { ...device, reachable: false, known: true });
  }
}

function persistKnownDevices() {
  const known = [];
  for (const device of state.devices.values()) {
    if (!device.mac || !device.known) continue;
    known.push({
      mac: device.mac,
      label: device.label || "",
      hostname: device.hostname || "",
      ip: device.ip || "",
      interfaceId: device.interfaceId || "",
      interfaceName: device.interfaceName || "",
      lastSeen: device.lastSeen || "",
    });
  }
  localStorage.setItem(STORAGE_KEY, JSON.stringify(known));
}

// ---------- interfaces ----------

function selectedInterface() {
  return state.interfaces.find((item) => item.id === el.interfaceSelect.value);
}

function renderInterfaces() {
  // Empty on first render (default to first real interface); holds the user's
  // choice on later refreshes (preserve it).
  const previous = el.interfaceSelect.value;
  el.interfaceSelect.innerHTML = "";

  const auto = document.createElement("option");
  auto.value = "auto";
  auto.textContent = "Automatic route";
  el.interfaceSelect.appendChild(auto);

  for (const item of state.interfaces) {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = `${item.name} — ${item.ipv4}/${item.prefixLength}`;
    el.interfaceSelect.appendChild(option);
  }

  const hasPrevious = Array.from(el.interfaceSelect.options).some((o) => o.value === previous);
  el.interfaceSelect.value = hasPrevious ? previous : (state.interfaces[0]?.id || "auto");
  el.interfaceCount.textContent = `${state.interfaces.length} interface${state.interfaces.length === 1 ? "" : "s"}`;
  renderInterfaceDetail();
}

function renderInterfaceDetail() {
  const item = selectedInterface();
  el.scan.disabled = state.busy || !item; // scanning needs a concrete interface
  if (!item) {
    el.interfaceDetail.innerHTML = `
      <dl class="kv">
        <div><dt>Mode</dt><dd>Automatic route</dd></div>
        <div><dt>Scan</dt><dd>Pick a specific interface to scan its subnet</dd></div>
      </dl>`;
    return;
  }
  el.interfaceDetail.innerHTML = `
    <dl class="kv">
      <div><dt>Name</dt><dd>${escapeHTML(item.name)}</dd></div>
      <div><dt>Subnet</dt><dd>${escapeHTML(item.ipv4)}/${item.prefixLength}</dd></div>
      <div><dt>Broadcast</dt><dd>${escapeHTML(item.broadcast)}</dd></div>
      <div><dt>Adapter MAC</dt><dd>${escapeHTML(item.hardwareAddress || "Unavailable")}</dd></div>
    </dl>`;
}

async function loadInterfaces() {
  setBusy(true);
  try {
    const response = await fetch("/api/interfaces");
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "Failed to load interfaces");
    state.interfaces = payload.interfaces || [];
    renderInterfaces();
    setStatus(el.status, "Interfaces refreshed.", "neutral");
  } catch (error) {
    setStatus(el.status, error.message, "error");
  } finally {
    setBusy(false);
  }
}

// ---------- scanning ----------

async function scan() {
  const iface = selectedInterface();
  if (!iface) {
    setStatus(el.scanStatus, "Choose a specific interface to scan.", "error");
    return;
  }

  setBusy(true);
  setStatus(el.scanStatus, `Scanning ${iface.name} (${iface.ipv4}/${iface.prefixLength})…`, "neutral");
  el.scanSummary.textContent = "";

  try {
    const response = await fetch("/api/scan", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ interfaceId: iface.id }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "Scan failed");
    mergeScanResults(iface, payload.devices || []);
    el.scanSummary.textContent = `${payload.reachable} of ${payload.hostsScanned} hosts responded`;
    setStatus(
      el.scanStatus,
      payload.reachable === 0
        ? "No hosts responded. Offline devices can't be discovered — add a known one manually below."
        : `Found ${payload.reachable} reachable device${payload.reachable === 1 ? "" : "s"}.`,
      payload.reachable === 0 ? "neutral" : "success",
    );
  } catch (error) {
    setStatus(el.scanStatus, error.message, "error");
  } finally {
    setBusy(false);
  }
}

function findDeviceByAddress(ip, interfaceId) {
  for (const [key, device] of state.devices) {
    if (device.ip === ip && device.interfaceId === interfaceId) return key;
  }
  return null;
}

function mergeScanResults(iface, discovered) {
  // Mark previously-seen devices on this interface as stale before applying results.
  for (const device of state.devices.values()) {
    if (device.interfaceId === iface.id) device.reachable = false;
  }

  for (const found of discovered) {
    const macNow = found.mac ? normalizeMac(found.mac) : "";
    // Reconcile with any prior record of this host — whether it was keyed by MAC
    // (stable identity, survives an IP change) or by address (a MAC-less sighting)
    // — so a host can't split into two rows across scans.
    const macKey = macNow ? `mac:${macNow}` : null;
    const addrKey = findDeviceByAddress(found.ip, found.interfaceId);
    const prior = (macKey && state.devices.get(macKey)) || (addrKey && state.devices.get(addrKey)) || null;

    const mac = macNow || (prior && prior.mac) || ""; // never drop a MAC we already learned
    const merged = {
      ...(prior || {}),
      ...found,
      mac,
      label: (prior && prior.label) || "",
      known: (prior && prior.known) || Boolean(mac),
    };

    // Collapse every stale key for this host down to one canonical row.
    if (addrKey) state.devices.delete(addrKey);
    if (macKey && macKey !== addrKey) state.devices.delete(macKey);
    const newKey = deviceKey(merged);
    if (state.selectedKey === addrKey || state.selectedKey === macKey) {
      state.selectedKey = newKey; // keep the user's selection stable across a rescan
    }
    state.devices.set(newKey, merged);
  }

  persistKnownDevices();
  renderDevices();
}

// ---------- device list ----------

function sortedDevices() {
  return Array.from(state.devices.values()).sort((a, b) => {
    if (a.reachable !== b.reachable) return a.reachable ? -1 : 1;
    return ipToNumber(a.ip) - ipToNumber(b.ip);
  });
}

function ipToNumber(ip) {
  const parts = String(ip || "").split(".").map(Number);
  if (parts.length !== 4 || parts.some((n) => Number.isNaN(n))) return Number.MAX_SAFE_INTEGER;
  return ((parts[0] << 24) >>> 0) + (parts[1] << 16) + (parts[2] << 8) + parts[3];
}

function renderDevices() {
  const devices = sortedDevices();
  el.deviceList.innerHTML = "";

  if (devices.length === 0) {
    el.deviceList.innerHTML = `<p class="empty">No devices yet. Choose an interface and scan, or add one with the advanced form below.</p>`;
    updateSelectedPanel();
    return;
  }

  for (const device of devices) {
    const key = deviceKey(device);
    const row = document.createElement("label");
    row.className = "device";
    row.dataset.key = key;
    if (key === state.selectedKey) row.dataset.selected = "true";

    const macText = device.mac
      ? escapeHTML(device.mac)
      : `<span class="warn">No MAC — can't wake yet</span>`;
    const statusText = device.reachable ? "Online" : device.known ? "Saved" : "Offline";
    const statusTone = device.reachable ? "online" : device.known ? "saved" : "offline";

    row.innerHTML = `
      <input type="radio" name="device" value="${escapeHTML(key)}" ${key === state.selectedKey ? "checked" : ""}>
      <span class="dot" data-state="${statusTone}" aria-hidden="true"></span>
      <span class="device-main">
        <span class="device-name">${escapeHTML(deviceName(device))}</span>
        <span class="device-meta">${escapeHTML(device.ip || "—")} · ${macText}</span>
      </span>
      <span class="device-side">
        <span class="badge" data-state="${statusTone}">${statusText}</span>
        <span class="device-seen">${escapeHTML(relativeSeen(device.lastSeen))}</span>
      </span>`;

    row.querySelector("input").addEventListener("change", () => selectDevice(key));
    el.deviceList.appendChild(row);
  }

  updateSelectedPanel();
}

// ---------- selection + wake ----------

function selectedDevice() {
  return state.selectedKey ? state.devices.get(state.selectedKey) : null;
}

// effectiveMac is the MAC we could wake with: the device's stored MAC, or a
// valid MAC currently typed into the "enter once to save" field.
function effectiveMac() {
  const device = selectedDevice();
  if (!device) return "";
  return device.mac || normalizeMac(el.macInput.value) || "";
}

function canWakeSelected() {
  return Boolean(effectiveMac());
}

function refreshWakeButton() {
  const wakeable = canWakeSelected();
  el.wakeSelected.disabled = state.busy || !wakeable;
  el.wakeSelected.title = wakeable ? "" : "Enter a MAC to enable waking";
}

function selectDevice(key) {
  state.selectedKey = key;
  for (const row of el.deviceList.querySelectorAll(".device")) {
    row.dataset.selected = row.dataset.key === key ? "true" : "false";
  }
  updateSelectedPanel();
}

function updateSelectedPanel() {
  const device = selectedDevice();
  if (!device) {
    el.selected.hidden = true;
    el.wakeSelected.disabled = true;
    return;
  }

  el.selected.hidden = false;
  el.selectedName.textContent = deviceName(device);
  el.selectedBadge.textContent = device.reachable ? "Online" : device.known ? "Saved" : "Offline";
  el.selectedBadge.dataset.state = device.reachable ? "online" : device.known ? "saved" : "offline";

  el.selectedDetail.innerHTML = `
    <div><dt>IP</dt><dd>${escapeHTML(device.ip || "—")}</dd></div>
    <div><dt>MAC</dt><dd>${escapeHTML(device.mac || "Unknown")}</dd></div>
    <div><dt>Hostname</dt><dd>${escapeHTML(device.hostname || "—")}</dd></div>
    <div><dt>Interface</dt><dd>${escapeHTML(device.interfaceName || "—")}</dd></div>
    <div><dt>Source</dt><dd>${escapeHTML(device.source || "saved")}</dd></div>
    <div><dt>Last seen</dt><dd>${escapeHTML(relativeSeen(device.lastSeen))}</dd></div>`;

  el.labelInput.value = device.label || "";
  el.macMissing.hidden = Boolean(device.mac);
  if (!device.mac) el.macInput.value = "";

  refreshWakeButton();
}

function persistDevice(device) {
  device.known = Boolean(device.mac);
  state.devices.set(deviceKey(device), device);
  persistKnownDevices();
}

// Apply the label + optional typed MAC from the selected panel back onto the device.
function applySelectedEdits() {
  const device = selectedDevice();
  if (!device) return null;

  device.label = el.labelInput.value.trim();

  if (!device.mac) {
    const mac = normalizeMac(el.macInput.value);
    if (!mac) {
      setStatus(el.status, "Enter a valid MAC (12 hex digits) to save this device.", "error");
      return null;
    }
    // Refuse to overwrite a different device already saved under this MAC.
    const clash = state.devices.get(`mac:${mac}`);
    if (clash && clash !== device) {
      setStatus(el.status, `MAC ${mac} is already saved as "${deviceName(clash)}" (${clash.ip || "no IP"}).`, "error");
      return null;
    }
    // Re-key the device now that it has a stable MAC identity.
    state.devices.delete(state.selectedKey);
    device.mac = mac;
    state.selectedKey = deviceKey(device);
  }

  persistDevice(device);
  renderDevices();
  selectDevice(state.selectedKey);
  return device;
}

async function wakeSelected() {
  const device = applySelectedEdits();
  if (!device || !device.mac) return;
  await sendWake(device.mac, device.interfaceId || el.interfaceSelect.value, deviceName(device));
}

async function wakeManual(event) {
  event.preventDefault();
  const mac = normalizeMac(el.manualMac.value);
  if (!mac) {
    setStatus(el.status, "Enter a valid MAC (12 hex digits).", "error");
    return;
  }
  await sendWake(mac, el.interfaceSelect.value, mac);
}

async function sendWake(mac, interfaceId, name) {
  setBusy(true);
  try {
    const response = await fetch("/api/wake", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        mac,
        interfaceId: interfaceId || "auto",
        port: Number(el.port.value || 9),
      }),
    });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "Wake request failed");
    setStatus(
      el.status,
      `Woke ${name}: sent ${payload.bytesSent} bytes to ${payload.broadcast}:${payload.port} via ${payload.interfaceName}.`,
      "success",
    );
  } catch (error) {
    setStatus(el.status, error.message, "error");
  } finally {
    setBusy(false);
  }
}

// ---------- wiring ----------

el.refresh.addEventListener("click", loadInterfaces);
el.scan.addEventListener("click", scan);
el.interfaceSelect.addEventListener("change", renderInterfaceDetail);
el.wakeSelected.addEventListener("click", wakeSelected);
el.macInput.addEventListener("input", refreshWakeButton);
el.manualForm.addEventListener("submit", wakeManual);

loadKnownDevices();
renderDevices();
loadInterfaces();
