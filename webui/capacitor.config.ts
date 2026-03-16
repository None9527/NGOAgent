import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'io.ngoclaw.agent',
  appName: 'NGOAgent',
  webDir: 'dist',
  server: {
    // Use http scheme so the WebView page is NOT treated as HTTPS.
    // This prevents Mixed Content blocking when calling http://192.168.x.x:19997.
    androidScheme: 'http',
  },
};

export default config;
