// Keeps the daily form's unsaved values on the phone (localStorage), per
// person and day, so a reload or a lost connection never loses what was
// typed (system-design §6.6). A draft belongs to the report version it was
// typed on: once a save moves the version on, the draft is dropped.
//
// The only custom script besides sw.js (AGENTS.md). Forms opt in with
// data-draft="<date>", data-draft-user="<uid>" and data-version="<n>".

const PREFIX = "kassa-draft:";

function storage() {
  try {
    return window.localStorage;
  } catch {
    return null; // private mode or storage disabled: no drafts
  }
}

const keyOf = (form) => `${PREFIX}${form.dataset.draftUser}:${form.dataset.draft}${form.dataset.draftEntry ? ":client:" + form.dataset.draftEntry + (form.dataset.draftEntry === "new" ? ":" + form.dataset.draftToken : "") : ""}`;

function read(store, key) {
  try {
    return JSON.parse(store.getItem(key) ?? "null");
  } catch {
    store.removeItem(key);
    return null;
  }
}

function fieldsOf(form) {
  const fields = {};
  for (const el of form.elements) {
    if (!el.name || el.type === "hidden" || el.type === "submit") continue;
    fields[el.name] = el.type === "checkbox" ? el.checked : el.value;
  }
  return fields;
}

function save(form) {
  const store = storage();
  if (!store) return;
  try {
    store.setItem(keyOf(form), JSON.stringify({ version: form.dataset.version, token: form.dataset.draftToken, fields: fieldsOf(form) }));
  } catch {
    // storage full: the form still works, just without a draft
  }
}

// restore puts a draft typed on this version back, firing the events
// Datastar's bindings listen to, so the live figures follow.
function restore(form) {
  const store = storage();
  if (!store) return;
  const key = keyOf(form);
  const draft = read(store, key);
  if (!draft) { if (form.dataset.draftEntry === "new") save(form); return; }
  if (draft.version !== form.dataset.version) {
    store.removeItem(key);
    return;
  }
  for (const [name, value] of Object.entries(draft.fields ?? {})) {
    const el = form.elements.namedItem(name);
    if (!el || el.type === "hidden") continue;
    if (el.type === "checkbox") {
      if (el.checked !== Boolean(value)) {
        el.checked = Boolean(value);
        // Datastar's checkbox binding listens to input; change for anything else.
        el.dispatchEvent(new Event("input", { bubbles: true }));
        el.dispatchEvent(new Event("change", { bubbles: true }));
      }
    } else if (typeof value === "string" && el.value !== value) {
      el.value = value;
      el.dispatchEvent(new Event("input", { bubbles: true }));
    }
  }
}

// drop forgets a draft once the form shows a newer saved version.
function drop(form) {
  const store = storage();
  if (!store) return;
  const key = keyOf(form);
  const draft = read(store, key);
  if (draft && draft.version !== form.dataset.version) store.removeItem(key);
}

const draftForm = (el) => el?.closest?.("form[data-draft]");

for (const type of ["input", "change"]) {
  document.addEventListener(type, (e) => {
    const form = draftForm(e.target);
    if (form && e.isTrusted) save(form);
  });
}

new MutationObserver((records) => {
  for (const r of records) {
    if (r.target instanceof HTMLFormElement && r.target.matches("form[data-draft]")) drop(r.target);
  }
}).observe(document.documentElement, { subtree: true, attributes: true, attributeFilter: ["data-version"] });

// After Datastar has bound the form, so restored values reach its signals.
function acknowledge(form) {
  if (!form.dataset.draftAck) return;
  const store = storage();
  if (!store) return;
  const key = `${PREFIX}${form.dataset.draftUser}:${form.dataset.draft}:client:new:${form.dataset.draftAck}`;
  const draft = read(store, key);
  if (draft?.token === form.dataset.draftAck) store.removeItem(key);
}

window.addEventListener("load", () => {
  document.querySelectorAll("form[data-draft]").forEach(acknowledge);
  setTimeout(() => document.querySelectorAll("form[data-draft]").forEach(restore), 0);
});
