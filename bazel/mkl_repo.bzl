"""mkl_repo produces a repo for MKL installation.

It relies on the installatin at mkl_root
"""

def mkl_repo(name, mkl_root):
    """generated a mkl repo at mkl_root"""
    native.new_local_repository(
        name = name,
        path = mkl_root,
    )
