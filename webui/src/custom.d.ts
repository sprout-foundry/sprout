declare module '*.svg' {
  import type * as React from 'react';

  export const ReactComponent: React.FunctionComponent<React.SVGProps<SVGSVGElement> & { title?: string }>;

  const src: string;
  export default src;
}

declare namespace globalThis {
  // eslint-disable-next-line no-var -- var is required in ambient declarations
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
