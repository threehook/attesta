// Runs in the renderer's isolated world: exposes the wallet's few operations and nothing else.
import { contextBridge, ipcRenderer } from "electron";
import type { WalletApi } from "../shared/api.js";

const channels = { list: "wallet:list", prepare: "wallet:prepare", approve: "wallet:approve", decline: "wallet:decline", link: "wallet:link" };

const api: WalletApi = {
  list: () => ipcRenderer.invoke(channels.list),
  prepare: (link) => ipcRenderer.invoke(channels.prepare, link),
  approve: (id) => ipcRenderer.invoke(channels.approve, id),
  decline: (id) => ipcRenderer.invoke(channels.decline, id),
  onLink: (callback) => {
    ipcRenderer.on(channels.link, (_event, link: string) => callback(link));
  },
};

contextBridge.exposeInMainWorld("wallet", api);
