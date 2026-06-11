const state = {
  interfaces: [],
  loading: false,
};

const elements = {
  form: document.querySelector("#wake-form"),
  mac: document.querySelector("#mac"),
  port: document.querySelector("#port"),
  interfaceSelect: document.querySelector("#interface"),
  interfaceDetail: document.querySelector("#interface-detail"),
  interfaceCount: document.querySelector("#interface-count"),
  refresh: document.querySelector("#refresh"),
  wake: document.querySelector("#wake"),
  status: document.querySelector("#status"),
};

function setStatus(message, tone = "neutral") {
  elements.status.textContent = message;
  elements.status.dataset.tone = tone;
}

function setLoading(isLoading) {
  state.loading = isLoading;
  elements.refresh.disabled = isLoading;
  elements.wake.disabled = isLoading;
}

function selectedInterface() {
  const id = elements.interfaceSelect.value;
  return state.interfaces.find((item) => item.id === id);
}

function renderInterfaces() {
  const previous = elements.interfaceSelect.value || "auto";
  elements.interfaceSelect.innerHTML = "";

  const auto = document.createElement("option");
  auto.value = "auto";
  auto.textContent = "Automatic route";
  elements.interfaceSelect.appendChild(auto);

  for (const item of state.interfaces) {
    const option = document.createElement("option");
    option.value = item.id;
    option.textContent = `${item.name} - ${item.ipv4}/${item.prefixLength}`;
    elements.interfaceSelect.appendChild(option);
  }

  const hasPrevious = Array.from(elements.interfaceSelect.options).some((option) => option.value === previous);
  elements.interfaceSelect.value = hasPrevious ? previous : "auto";
  elements.interfaceCount.textContent = `${state.interfaces.length} interface${state.interfaces.length === 1 ? "" : "s"}`;
  renderInterfaceDetail();
}

function renderInterfaceDetail() {
  const item = selectedInterface();
  if (!item) {
    elements.interfaceDetail.innerHTML = `
      <dl>
        <div><dt>Mode</dt><dd>Automatic route</dd></div>
        <div><dt>Broadcast</dt><dd>255.255.255.255</dd></div>
      </dl>
    `;
    return;
  }

  elements.interfaceDetail.innerHTML = `
    <dl>
      <div><dt>Name</dt><dd>${escapeHTML(item.name)}</dd></div>
      <div><dt>Address</dt><dd>${escapeHTML(item.ipv4)}/${item.prefixLength}</dd></div>
      <div><dt>Broadcast</dt><dd>${escapeHTML(item.broadcast)}</dd></div>
      <div><dt>MAC</dt><dd>${escapeHTML(item.hardwareAddress || "Unavailable")}</dd></div>
    </dl>
  `;
}

async function loadInterfaces() {
  setLoading(true);
  try {
    const response = await fetch("/api/interfaces");
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Failed to load interfaces");
    }

    state.interfaces = payload.interfaces || [];
    renderInterfaces();
    setStatus("Interfaces refreshed.", "neutral");
  } catch (error) {
    setStatus(error.message, "error");
  } finally {
    setLoading(false);
  }
}

async function wake(event) {
  event.preventDefault();
  setLoading(true);

  try {
    const request = {
      mac: elements.mac.value.trim(),
      interfaceId: elements.interfaceSelect.value,
      port: Number(elements.port.value || 9),
    };

    const response = await fetch("/api/wake", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Wake request failed");
    }

    setStatus(`Sent ${payload.bytesSent} bytes to ${payload.broadcast}:${payload.port} via ${payload.interfaceName}.`, "success");
  } catch (error) {
    setStatus(error.message, "error");
  } finally {
    setLoading(false);
  }
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

elements.form.addEventListener("submit", wake);
elements.refresh.addEventListener("click", loadInterfaces);
elements.interfaceSelect.addEventListener("change", renderInterfaceDetail);

loadInterfaces();
