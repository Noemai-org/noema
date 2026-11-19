def double(amount_minor: int) -> int:
    return amount_minor * 2


def millis(elapsed: int) -> int:
    return elapsed * 1000


def percent(count: int) -> int:
    return count // 100


def scale(amount_minor: int, factor: int) -> int:
    return amount_minor * factor
