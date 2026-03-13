import { AgentClient } from './src/grpc/client.js';
(async () => {
  const c = new AgentClient('localhost:50051');
  const h = await c.healthCheck();
  console.log('Health:', h.model);
  const m = await c.listModels();
  console.log('Models:', m.currentModel);
})();
