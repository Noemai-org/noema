def to_major(amount_minor: int) -> int:
    return amount_minor // 100


def to_minor(amount: int) -> int:
    return amount * 10**2
