import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../', import.meta.url));
export default defineConfig({root, plugins:[react()], define:{'process.env.NEXT_PUBLIC_API_URL':'""'}, resolve:{alias:[
 {find:'@/contexts/auth-context',replacement:root+'profile-refresh-browser/auth.js'},
 {find:'@/components/layout/page-header',replacement:root+'profile-refresh-browser/header.jsx'},
 {find:'@',replacement:root}
]}, server:{host:'127.0.0.1',port:4179,strictPort:true}});
