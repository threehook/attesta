// Runs in the renderer's isolated world: exposes the wallet's few operations and nothing else.
import { contextBridge, ipcRenderer } from "electron";
import type { WalletApi } from "../shared/api.js";

const channels = {
  list: "wallet:list",
  prepare: "wallet:prepare",
  approve: "wallet:approve",
  decline: "wallet:decline",
  settings: "wallet:settings",
  forgetApplication: "wallet:forget-application",
  forgetSignIn: "wallet:forget-sign-in",
  link: "wallet:link",
  settingsChanged: "wallet:settings-changed",
};

const api: WalletApi = {
  list: () => ipcRenderer.invoke(channels.list),
  prepare: (link) => ipcRenderer.invoke(channels.prepare, link),
  approve: (id, remember) => ipcRenderer.invoke(channels.approve, id, remember),
  decline: (id) => ipcRenderer.invoke(channels.decline, id),
  settings: () => ipcRenderer.invoke(channels.settings),
  forgetApplication: (application) => ipcRenderer.invoke(channels.forgetApplication, application),
  forgetSignIn: (choice) => ipcRenderer.invoke(channels.forgetSignIn, choice),
  onLink: (callback) => {
    ipcRenderer.on(channels.link, (_event, result) => callback(result));
  },
  onSettingsChanged: (callback) => {
    ipcRenderer.on(channels.settingsChanged, () => callback());
  },
};

contextBridge.exposeInMainWorld("wallet", api);
