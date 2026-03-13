import { render, Box, Text } from 'ink';
import React from 'react';

console.clear();
const App = () => <Box borderStyle="round"><Text>Hello World</Text></Box>;
render(React.createElement(App));
