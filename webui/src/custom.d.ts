declare module '*.svg' {
  import type { FunctionComponent, SVGProps } from 'react';

  export const ReactComponent: FunctionComponent<SVGProps<SVGSVGElement> & { title?: string }>;

  const src: string;
  export default src;
}

declare namespace globalThis {
  // eslint-disable-next-line no-var -- var is required in ambient declarations
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
