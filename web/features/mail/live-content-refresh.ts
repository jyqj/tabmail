"use client";
import { createContext } from "react";

// A workspace-local invalidation signal, not content or permission. Only an
// already-mounted live reader consumes it; disclosure remains caller-owned.
export const LiveContentRefresh = createContext(0);
