// The backend client shared across the app.
import { ApiClient } from "@attesta/client";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export const apiClient = new ApiClient(API_BASE_URL);
