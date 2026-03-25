import json
import time
from datetime import datetime

# 记录开始时间
start_time = datetime.now()
start_iso = start_time.isoformat()

# 1. 计算斐波那契数列前30项
def fibonacci_list(n):
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[i-1] + fib[i-2])
    return fib[:n]

fib_30 = fibonacci_list(30)

# 2. 计算斐波那契第35项
def fibonacci_nth(n):
    if n <= 0:
        return 0
    elif n == 1:
        return 1
    a, b = 0, 1
    for _ in range(2, n + 1):
        a, b = b, a + b
    return b

fib_35 = fibonacci_nth(35)

# 3. 找出1到10000之间的所有质数
def is_prime(n):
    if n < 2:
        return False
    if n == 2:
        return True
    if n % 2 == 0:
        return False
    for i in range(3, int(n**0.5) + 1, 2):
        if n % i == 0:
            return False
    return True

primes = [n for n in range(1, 10001) if is_prime(n)]
primes_count = len(primes)
sample_primes = primes[:10]

# 记录结束时间
end_time = datetime.now()
end_iso = end_time.isoformat()
duration_ms = int((end_time - start_time).total_seconds() * 1000)

# 构建结果JSON
result = {
    "agent_id": "Subagent-Test-1",
    "task": "computation",
    "status": "completed",
    "timestamp": datetime.now().isoformat(),
    "start_time": start_iso,
    "end_time": end_iso,
    "duration_ms": duration_ms,
    "data": {
        "fibonacci": fib_30,
        "fibonacci_35": fib_35,
        "primes_count": primes_count,
        "sample_primes": sample_primes
    }
}

# 保存结果文件
output_path = "/home/none/.ngoagent/workspace/subagent_compute_result.json"
with open(output_path, 'w', encoding='utf-8') as f:
    json.dump(result, f, indent=2, ensure_ascii=False)

print(f"结果已保存到: {output_path}")
print(f"执行时长: {duration_ms} ms")
print(f"斐波那契前30项: {fib_30}")
print(f"斐波那契第35项: {fib_35}")
print(f"质数总数: {primes_count}")
print(f"前10个质数: {sample_primes}")
