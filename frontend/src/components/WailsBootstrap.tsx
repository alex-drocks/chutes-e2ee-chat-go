'use client';

import { useEffect } from 'react';
import { installWailsBridge } from '@/lib/wailsBridge';

if (typeof window !== 'undefined') {
  installWailsBridge();
}

export default function WailsBootstrap() {
  useEffect(() => {
    installWailsBridge();
  }, []);

  return null;
}
